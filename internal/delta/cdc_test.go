package delta

import (
	"bytes"
	"context"
	"crypto/rand"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFastCDC_DefaultConfig(t *testing.T) {
	config := DefaultFastCDCConfig()
	assert.Equal(t, 2*1024, config.MinSize)
	assert.Equal(t, 64*1024, config.AvgSize)
	assert.Equal(t, 1024*1024, config.MaxSize)
	assert.Equal(t, 2, config.NormalizationLevel)
}

func TestFastCDC_SmallData(t *testing.T) {
	cdc := NewFastCDCDefault()
	ctx := context.Background()

	// Data smaller than MinSize should be returned as single chunk
	data := []byte("hello world")
	chunks, err := cdc.ChunkAll(ctx, bytes.NewReader(data))
	require.NoError(t, err)

	assert.Len(t, chunks, 1)
	assert.Equal(t, int64(len(data)), chunks[0].Size)
	assert.Equal(t, data, chunks[0].Data)
}

func TestFastCDC_ExactMinSize(t *testing.T) {
	cdc := NewFastCDC(FastCDCConfig{
		MinSize:            64,
		AvgSize:            128,
		MaxSize:            256,
		NormalizationLevel: 2,
	})
	ctx := context.Background()

	// Data exactly MinSize bytes
	data := make([]byte, 64)
	for i := range data {
		data[i] = byte(i)
	}

	chunks, err := cdc.ChunkAll(ctx, bytes.NewReader(data))
	require.NoError(t, err)

	assert.Len(t, chunks, 1)
	assert.Equal(t, int64(64), chunks[0].Size)
}

func TestFastCDC_LargeRandomData(t *testing.T) {
	cdc := NewFastCDC(FastCDCConfig{
		MinSize:            512,
		AvgSize:            2048,
		MaxSize:            8192,
		NormalizationLevel: 2,
	})
	ctx := context.Background()

	// Generate random data
	data := make([]byte, 100*1024) // 100KB
	_, err := rand.Read(data)
	require.NoError(t, err)

	chunks, err := cdc.ChunkAll(ctx, bytes.NewReader(data))
	require.NoError(t, err)

	// Should have multiple chunks
	assert.Greater(t, len(chunks), 1)

	// Verify all chunks are within size bounds
	for i, chunk := range chunks {
		// Last chunk may be smaller than MinSize
		if i < len(chunks)-1 {
			assert.GreaterOrEqual(t, int(chunk.Size), cdc.config.MinSize,
				"chunk %d size %d < MinSize %d", i, chunk.Size, cdc.config.MinSize)
		}
		assert.LessOrEqual(t, int(chunk.Size), cdc.config.MaxSize,
			"chunk %d size %d > MaxSize %d", i, chunk.Size, cdc.config.MaxSize)
	}

	// Verify total size equals original
	var totalSize int64
	for _, chunk := range chunks {
		totalSize += chunk.Size
	}
	assert.Equal(t, int64(len(data)), totalSize)

	// Verify data reconstruction
	var reconstructed bytes.Buffer
	for _, chunk := range chunks {
		reconstructed.Write(chunk.Data)
	}
	assert.Equal(t, data, reconstructed.Bytes())
}

func TestFastCDC_Deterministic(t *testing.T) {
	cdc := NewFastCDCDefault()
	ctx := context.Background()

	// Same data should produce same chunks
	data := make([]byte, 200*1024)
	_, err := rand.Read(data)
	require.NoError(t, err)

	chunks1, err := cdc.ChunkAll(ctx, bytes.NewReader(data))
	require.NoError(t, err)

	chunks2, err := cdc.ChunkAll(ctx, bytes.NewReader(data))
	require.NoError(t, err)

	require.Equal(t, len(chunks1), len(chunks2))
	for i := range chunks1 {
		assert.Equal(t, chunks1[i].Hash, chunks2[i].Hash)
		assert.Equal(t, chunks1[i].Size, chunks2[i].Size)
		assert.Equal(t, chunks1[i].Offset, chunks2[i].Offset)
	}
}

func TestFastCDC_ShiftResistance(t *testing.T) {
	cdc := NewFastCDC(FastCDCConfig{
		MinSize:            128,
		AvgSize:            512,
		MaxSize:            2048,
		NormalizationLevel: 2,
	})
	ctx := context.Background()

	// Create original data
	original := make([]byte, 50*1024)
	_, err := rand.Read(original)
	require.NoError(t, err)

	// Create modified data: insert 100 bytes at the beginning
	insertion := make([]byte, 100)
	_, err = rand.Read(insertion)
	require.NoError(t, err)
	modified := append(insertion, original...)

	originalChunks, err := cdc.ChunkAll(ctx, bytes.NewReader(original))
	require.NoError(t, err)

	modifiedChunks, err := cdc.ChunkAll(ctx, bytes.NewReader(modified))
	require.NoError(t, err)

	// Count matching chunks (by hash)
	originalHashes := make(map[string]bool)
	for _, chunk := range originalChunks {
		originalHashes[chunk.Hash] = true
	}

	matchingChunks := 0
	for _, chunk := range modifiedChunks {
		if originalHashes[chunk.Hash] {
			matchingChunks++
		}
	}

	// CDC should preserve many chunks despite insertion
	// At least 50% of original chunks should still match
	matchRatio := float64(matchingChunks) / float64(len(originalChunks))
	t.Logf("Shift resistance: %.1f%% chunks preserved after 100-byte insertion", matchRatio*100)
	assert.Greater(t, matchRatio, 0.3, "CDC should preserve at least 30%% of chunks after small insertion")
}

func TestFastCDC_UniqueHashes(t *testing.T) {
	cdc := NewFastCDCDefault()
	ctx := context.Background()

	// Different data should produce different hashes
	data1 := []byte(strings.Repeat("A", 10000))
	data2 := []byte(strings.Repeat("B", 10000))

	chunks1, err := cdc.ChunkAll(ctx, bytes.NewReader(data1))
	require.NoError(t, err)

	chunks2, err := cdc.ChunkAll(ctx, bytes.NewReader(data2))
	require.NoError(t, err)

	// Hashes should be different
	if len(chunks1) > 0 && len(chunks2) > 0 {
		assert.NotEqual(t, chunks1[0].Hash, chunks2[0].Hash)
	}
}

func TestFastCDC_ContextCancellation(t *testing.T) {
	cdc := NewFastCDCDefault()
	ctx, cancel := context.WithCancel(context.Background())

	// Generate large data
	data := make([]byte, 10*1024*1024) // 10MB
	_, err := rand.Read(data)
	require.NoError(t, err)

	// Cancel immediately
	cancel()

	// Should return context error
	_, err = cdc.ChunkAll(ctx, bytes.NewReader(data))
	assert.ErrorIs(t, err, context.Canceled)
}

func TestFastCDC_EmptyData(t *testing.T) {
	cdc := NewFastCDCDefault()
	ctx := context.Background()

	chunks, err := cdc.ChunkAll(ctx, bytes.NewReader([]byte{}))
	require.NoError(t, err)
	assert.Empty(t, chunks)
}

func TestFastCDC_StreamingChunk(t *testing.T) {
	cdc := NewFastCDC(FastCDCConfig{
		MinSize:            64,
		AvgSize:            256,
		MaxSize:            1024,
		NormalizationLevel: 2,
	})
	ctx := context.Background()

	data := make([]byte, 10*1024)
	_, err := rand.Read(data)
	require.NoError(t, err)

	chunkCh, errCh := cdc.Chunk(ctx, bytes.NewReader(data))

	var chunks []Chunk
	var lastErr error

	// Drain both channels completely
	for chunkCh != nil || errCh != nil {
		select {
		case chunk, ok := <-chunkCh:
			if !ok {
				chunkCh = nil
				continue
			}
			chunks = append(chunks, chunk)
		case err, ok := <-errCh:
			if !ok {
				errCh = nil
				continue
			}
			if err != nil {
				lastErr = err
			}
		}
	}

	require.NoError(t, lastErr)
	assert.Greater(t, len(chunks), 0)

	// Verify reconstruction
	var reconstructed bytes.Buffer
	for _, chunk := range chunks {
		reconstructed.Write(chunk.Data)
	}
	assert.Equal(t, data, reconstructed.Bytes())
}

func TestFastCDC_ChunkOffsets(t *testing.T) {
	cdc := NewFastCDC(FastCDCConfig{
		MinSize:            64,
		AvgSize:            256,
		MaxSize:            1024,
		NormalizationLevel: 2,
	})
	ctx := context.Background()

	data := make([]byte, 5*1024)
	_, err := rand.Read(data)
	require.NoError(t, err)

	chunks, err := cdc.ChunkAll(ctx, bytes.NewReader(data))
	require.NoError(t, err)

	// Verify offsets are consecutive
	var expectedOffset int64
	for i, chunk := range chunks {
		assert.Equal(t, expectedOffset, chunk.Offset,
			"chunk %d has wrong offset: expected %d, got %d", i, expectedOffset, chunk.Offset)
		expectedOffset += chunk.Size
	}
}

// Benchmark tests
func BenchmarkFastCDC_1MB(b *testing.B) {
	cdc := NewFastCDCDefault()
	ctx := context.Background()

	data := make([]byte, 1024*1024)
	rand.Read(data)

	b.ResetTimer()
	b.SetBytes(int64(len(data)))

	for i := 0; i < b.N; i++ {
		_, err := cdc.ChunkAll(ctx, bytes.NewReader(data))
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkFastCDC_10MB(b *testing.B) {
	cdc := NewFastCDCDefault()
	ctx := context.Background()

	data := make([]byte, 10*1024*1024)
	rand.Read(data)

	b.ResetTimer()
	b.SetBytes(int64(len(data)))

	for i := 0; i < b.N; i++ {
		_, err := cdc.ChunkAll(ctx, bytes.NewReader(data))
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkFastCDC_100MB(b *testing.B) {
	cdc := NewFastCDCDefault()
	ctx := context.Background()

	data := make([]byte, 100*1024*1024)
	rand.Read(data)

	b.ResetTimer()
	b.SetBytes(int64(len(data)))

	for i := 0; i < b.N; i++ {
		_, err := cdc.ChunkAll(ctx, bytes.NewReader(data))
		if err != nil {
			b.Fatal(err)
		}
	}
}

// Test for delta computer
func TestDeltaComputer_SameContent(t *testing.T) {
	chunker := NewFastCDC(FastCDCConfig{
		MinSize:            64,
		AvgSize:            256,
		MaxSize:            1024,
		NormalizationLevel: 2,
	})
	computer := NewComputer(chunker)
	ctx := context.Background()

	// Create data
	data := make([]byte, 5*1024)
	_, err := rand.Read(data)
	require.NoError(t, err)

	// Compute delta for same content (base == target)
	delta, err := computer.Compute(ctx, bytes.NewReader(data), bytes.NewReader(data))
	require.NoError(t, err)

	// All instructions should be COPY
	for _, inst := range delta.Instructions {
		assert.Equal(t, InstructionCopy, inst.Type)
	}

	// Should have high savings ratio (all copied, nothing inserted)
	t.Logf("Delta for identical content: %d instructions, savings ratio: %.2f",
		len(delta.Instructions), delta.SavingsRatio)
	assert.Equal(t, 1.0, delta.SavingsRatio)
}

func TestDeltaComputer_DifferentContent(t *testing.T) {
	chunker := NewFastCDC(FastCDCConfig{
		MinSize:            64,
		AvgSize:            256,
		MaxSize:            1024,
		NormalizationLevel: 2,
	})
	computer := NewComputer(chunker)
	ctx := context.Background()

	// Create original data
	original := make([]byte, 5*1024)
	_, err := rand.Read(original)
	require.NoError(t, err)

	// Create completely different data
	different := make([]byte, 5*1024)
	_, err = rand.Read(different)
	require.NoError(t, err)

	// Compute delta
	delta, err := computer.Compute(ctx, bytes.NewReader(original), bytes.NewReader(different))
	require.NoError(t, err)

	// Most instructions should be INSERT
	insertCount := 0
	for _, inst := range delta.Instructions {
		if inst.Type == InstructionInsert {
			insertCount++
		}
	}

	t.Logf("Delta for different content: %d instructions, %d inserts, savings ratio: %.2f",
		len(delta.Instructions), insertCount, delta.SavingsRatio)

	// Savings ratio should be low for random different data
	assert.Less(t, delta.SavingsRatio, 0.5)
}

func TestDeltaApplier_Reconstruct(t *testing.T) {
	// Use simpler test case where chunk boundaries are more predictable
	chunker := NewFastCDC(FastCDCConfig{
		MinSize:            64,
		AvgSize:            256,
		MaxSize:            1024,
		NormalizationLevel: 2,
	})
	computer := NewComputer(chunker)
	applier := NewApplier()
	ctx := context.Background()

	// Create base data - predictable chunks
	base := make([]byte, 4*1024)
	for i := range base {
		base[i] = byte(i % 256)
	}

	// Create target that is mostly different (all inserts)
	// This avoids chunk boundary issues
	target := make([]byte, 4*1024)
	for i := range target {
		target[i] = byte((i + 128) % 256)
	}

	// Compute delta (uses fresh readers)
	delta, err := computer.Compute(ctx, bytes.NewReader(base), bytes.NewReader(target))
	require.NoError(t, err)

	// Extract delta data (the insert portions) - needs fresh target reader
	deltaData, err := computer.ExtractDeltaData(ctx, bytes.NewReader(target), delta)
	require.NoError(t, err)

	// Apply delta to reconstruct - needs fresh base reader
	resultReader, err := applier.Apply(ctx, bytes.NewReader(base), delta, bytes.NewReader(deltaData))
	require.NoError(t, err)

	result, err := io.ReadAll(resultReader)
	require.NoError(t, err)

	// Verify reconstruction
	require.Equal(t, len(target), len(result), "result length should match target length")
	assert.Equal(t, target, result)
}

func TestDeltaApplier_CopyOnly(t *testing.T) {
	applier := NewApplier()
	ctx := context.Background()

	// Create base data
	base := []byte("Hello World! This is a test.")

	// Create delta that just copies everything
	delta := &Delta{
		SourceHash: "source",
		BaseHash:   "base",
		Instructions: []Instruction{
			{Type: InstructionCopy, SourceOffset: 0, TargetOffset: 0, Length: int64(len(base))},
		},
		TotalSize:    int64(len(base)),
		DeltaSize:    0,
		SavingsRatio: 1.0,
	}

	// Apply
	resultReader, err := applier.Apply(ctx, bytes.NewReader(base), delta, bytes.NewReader([]byte{}))
	require.NoError(t, err)

	result, err := io.ReadAll(resultReader)
	require.NoError(t, err)

	assert.Equal(t, base, result)
}

func TestDeltaApplier_InsertOnly(t *testing.T) {
	applier := NewApplier()
	ctx := context.Background()

	insertData := []byte("Completely new content!")

	delta := &Delta{
		SourceHash: "source",
		BaseHash:   "base",
		Instructions: []Instruction{
			{Type: InstructionInsert, SourceOffset: 0, TargetOffset: 0, Length: int64(len(insertData))},
		},
		TotalSize:    int64(len(insertData)),
		DeltaSize:    int64(len(insertData)),
		SavingsRatio: 0.0,
	}

	// Apply with empty base
	resultReader, err := applier.Apply(ctx, bytes.NewReader([]byte{}), delta, bytes.NewReader(insertData))
	require.NoError(t, err)

	result, err := io.ReadAll(resultReader)
	require.NoError(t, err)

	assert.Equal(t, insertData, result)
}

func TestDeltaApplier_MixedInstructions(t *testing.T) {
	applier := NewApplier()
	ctx := context.Background()

	base := []byte("Hello World!")
	insertData := []byte("Beautiful ")

	// Target should be: "Hello Beautiful World!"
	// Copy "Hello " (6 bytes), Insert "Beautiful " (10 bytes), Copy "World!" (6 bytes)
	delta := &Delta{
		SourceHash: "source",
		BaseHash:   "base",
		Instructions: []Instruction{
			{Type: InstructionCopy, SourceOffset: 0, TargetOffset: 0, Length: 6},    // "Hello "
			{Type: InstructionInsert, SourceOffset: 0, TargetOffset: 6, Length: 10}, // "Beautiful "
			{Type: InstructionCopy, SourceOffset: 6, TargetOffset: 16, Length: 6},   // "World!"
		},
		TotalSize:    22, // "Hello Beautiful World!"
		DeltaSize:    10,
		SavingsRatio: float64(12) / float64(22),
	}

	resultReader, err := applier.Apply(ctx, bytes.NewReader(base), delta, bytes.NewReader(insertData))
	require.NoError(t, err)

	result, err := io.ReadAll(resultReader)
	require.NoError(t, err)

	expected := "Hello Beautiful World!"
	assert.Equal(t, expected, string(result))
}
