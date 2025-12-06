# Alexander Storage Terraform Module

Terraform module for deploying Alexander Storage on various cloud providers.

## Features

- Multi-cloud support (AWS, GCP, Azure)
- PostgreSQL or embedded SQLite database
- Redis caching (optional)
- Auto-scaling support
- TLS/SSL configuration
- Backup automation
- Monitoring integration

## Quick Start

```hcl
module "alexander" {
  source = "./modules/aws"
  
  name        = "alexander-storage"
  environment = "production"
  
  # Compute
  instance_type = "t3.medium"
  min_size      = 2
  max_size      = 10
  
  # Database
  database_type     = "postgresql"
  postgres_host     = "db.example.com"
  postgres_database = "alexander"
  
  # Storage
  storage_path = "/data/alexander"
  
  # Network
  vpc_id     = "vpc-12345678"
  subnet_ids = ["subnet-a", "subnet-b", "subnet-c"]
}
```

## Requirements

| Name | Version |
|------|---------|
| terraform | >= 1.0 |
| aws | >= 5.0 |
| google | >= 5.0 |
| azurerm | >= 3.0 |

## Providers

| Name | Version |
|------|---------|
| aws | >= 5.0 |
| google | >= 5.0 |
| azurerm | >= 3.0 |

## Modules

- `modules/aws` - AWS deployment (EC2, ECS, EKS)
- `modules/gcp` - GCP deployment (GCE, GKE, Cloud Run)
- `modules/azure` - Azure deployment (AKS, Container Apps)

## Inputs

| Name | Description | Type | Default | Required |
|------|-------------|------|---------|:--------:|
| name | Name prefix for resources | `string` | `"alexander"` | no |
| environment | Environment name | `string` | n/a | yes |
| instance_type | Instance type for compute | `string` | `"t3.small"` | no |
| min_size | Minimum number of instances | `number` | `1` | no |
| max_size | Maximum number of instances | `number` | `5` | no |
| database_type | Database type (sqlite/postgresql) | `string` | `"sqlite"` | no |
| enable_redis | Enable Redis caching | `bool` | `false` | no |
| storage_path | Path for blob storage | `string` | `"/data/blobs"` | no |
| tags | Additional tags | `map(string)` | `{}` | no |

## Outputs

| Name | Description |
|------|-------------|
| endpoint | Alexander Storage endpoint URL |
| access_key_id | Admin access key ID |
| secret_access_key | Admin secret access key (sensitive) |
| database_endpoint | Database connection endpoint |

## Examples

See the `examples/` directory for complete examples:

- `examples/aws-simple` - Simple AWS deployment
- `examples/aws-production` - Production AWS deployment with HA
- `examples/gcp-gke` - GKE deployment
- `examples/azure-aks` - AKS deployment
