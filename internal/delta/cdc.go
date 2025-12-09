package delta

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
)

// FastCDCConfig holds configuration for the FastCDC chunking algorithm.
type FastCDCConfig struct {
	// MinSize is the minimum chunk size (default: 2KB).
	MinSize int

	// AvgSize is the average/target chunk size (default: 64KB).
	AvgSize int

	// MaxSize is the maximum chunk size (default: 1MB).
	MaxSize int

	// NormalizationLevel controls chunk size distribution (default: 2).
	// Higher values produce more uniform chunk sizes.
	NormalizationLevel int
}

// DefaultFastCDCConfig returns the default FastCDC configuration.
func DefaultFastCDCConfig() FastCDCConfig {
	return FastCDCConfig{
		MinSize:            2 * 1024,    // 2KB
		AvgSize:            64 * 1024,   // 64KB
		MaxSize:            1024 * 1024, // 1MB
		NormalizationLevel: 2,
	}
}

// FastCDC implements content-defined chunking using the FastCDC algorithm.
// FastCDC is faster than standard CDC while maintaining similar dedup ratios.
//
// Reference: "FastCDC: a Fast and Efficient Content-Defined Chunking Approach
// for Data Deduplication" by Wen Xia et al.
type FastCDC struct {
	config FastCDCConfig
	gear   [256]uint64 // Gear hash lookup table
}

// NewFastCDC creates a new FastCDC chunker with the given configuration.
func NewFastCDC(config FastCDCConfig) *FastCDC {
	cdc := &FastCDC{
		config: config,
	}
	cdc.initGear()
	return cdc
}

// NewFastCDCDefault creates a FastCDC chunker with default settings.
func NewFastCDCDefault() *FastCDC {
	return NewFastCDC(DefaultFastCDCConfig())
}

// initGear initializes the gear hash lookup table.
// Uses deterministic values for consistent chunking across runs.
func (c *FastCDC) initGear() {
	// Gear table values (deterministic for reproducibility)
	// These are pseudo-random values that provide good hash distribution
	seed := uint64(0x123456789ABCDEF0)
	for i := range c.gear {
		seed = seed*6364136223846793005 + 1442695040888963407
		c.gear[i] = seed
	}
}

// Chunk implements Chunker interface.
func (c *FastCDC) Chunk(ctx context.Context, reader io.Reader) (<-chan Chunk, <-chan error) {
	chunks := make(chan Chunk, 10) // Buffered for async processing
	errs := make(chan error, 1)

	go func() {
		defer close(chunks)
		defer close(errs)

		// Read all data first - this is simpler and avoids streaming bugs
		// For very large files, consider using ChunkReader which handles streaming better
		data, err := io.ReadAll(reader)
		if err != nil {
			errs <- err
			return
		}

		if len(data) == 0 {
			return
		}

		var offset int64
		remaining := data

		for len(remaining) > 0 {
			select {
			case <-ctx.Done():
				errs <- ctx.Err()
				return
			default:
			}

			// Find chunk boundary
			chunkSize := c.findBoundary(remaining)

			// Calculate hash of chunk
			hasher := sha256.New()
			hasher.Write(remaining[:chunkSize])
			hash := hex.EncodeToString(hasher.Sum(nil))

			chunk := Chunk{
				Hash:   hash,
				Offset: offset,
				Size:   int64(chunkSize),
				Data:   make([]byte, chunkSize),
			}
			copy(chunk.Data, remaining[:chunkSize])

			select {
			case <-ctx.Done():
				errs <- ctx.Err()
				return
			case chunks <- chunk:
			}

			offset += int64(chunkSize)
			remaining = remaining[chunkSize:]
		}
	}()

	return chunks, errs
}

// ChunkAll implements Chunker interface.
func (c *FastCDC) ChunkAll(ctx context.Context, reader io.Reader) ([]Chunk, error) {
	var result []Chunk

	chunkCh, errCh := c.Chunk(ctx, reader)

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case err := <-errCh:
			if err != nil {
				return nil, err
			}
		case chunk, ok := <-chunkCh:
			if !ok {
				// Check for final error
				select {
				case err := <-errCh:
					if err != nil {
						return nil, err
					}
				default:
				}
				return result, nil
			}
			result = append(result, chunk)
		}
	}
}

// findBoundary finds the chunk boundary using FastCDC algorithm.
// Returns the size of the chunk (boundary position).
func (c *FastCDC) findBoundary(data []byte) int {
	n := len(data)
	if n <= c.config.MinSize {
		return n
	}

	// Use different masks for different size regions
	// This normalizes chunk size distribution
	maskS := c.computeMask(c.config.AvgSize, c.config.NormalizationLevel-1)
	maskL := c.computeMask(c.config.AvgSize, c.config.NormalizationLevel+1)

	var hash uint64

	// Skip MinSize bytes (no boundary can occur before MinSize)
	i := c.config.MinSize

	// Region 1: MinSize to AvgSize - use harder mask (maskS)
	// This makes it less likely to find boundaries, pushing chunks toward AvgSize
	target := c.config.AvgSize
	if target > n {
		target = n
	}

	for i < target {
		hash = (hash << 1) + c.gear[data[i]]
		if hash&maskS == 0 {
			return i + 1
		}
		i++
	}

	// Region 2: AvgSize to MaxSize - use easier mask (maskL)
	// This makes it more likely to find boundaries
	target = c.config.MaxSize
	if target > n {
		target = n
	}

	for i < target {
		hash = (hash << 1) + c.gear[data[i]]
		if hash&maskL == 0 {
			return i + 1
		}
		i++
	}

	// Hit MaxSize (or end of data) without finding boundary
	// Return the smaller of MaxSize or data length
	if n < c.config.MaxSize {
		return n
	}
	return c.config.MaxSize
}

// computeMask computes the gear hash mask for a given average size.
// The number of 1-bits in the mask affects the probability of finding a boundary.
func (c *FastCDC) computeMask(avgSize, normLevel int) uint64 {
	// bits = log2(avgSize) adjusted by normalization level
	bits := 0
	size := avgSize
	for size > 1 {
		bits++
		size >>= 1
	}
	bits += normLevel

	if bits > 64 {
		bits = 64
	}
	if bits < 1 {
		bits = 1
	}

	return (uint64(1) << bits) - 1
}

// Ensure FastCDC implements Chunker
var _ Chunker = (*FastCDC)(nil)
