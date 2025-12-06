# Simple AWS Deployment Example
# This example deploys Alexander Storage with SQLite and basic configuration

provider "aws" {
  region = var.region
}

# Variables
variable "region" {
  default = "us-east-1"
}

variable "environment" {
  default = "dev"
}

# Data sources
data "aws_vpc" "default" {
  default = true
}

data "aws_subnets" "default" {
  filter {
    name   = "vpc-id"
    values = [data.aws_vpc.default.id]
  }
}

# Deploy Alexander Storage
module "alexander" {
  source = "../../modules/aws"
  
  name        = "alexander"
  environment = var.environment
  region      = var.region
  
  vpc_id     = data.aws_vpc.default.id
  subnet_ids = data.aws_subnets.default.ids
  
  # Use defaults (SQLite, no Redis, single instance)
  instance_type    = "t3.small"
  min_size         = 1
  max_size         = 1
  desired_capacity = 1
  
  database_type = "sqlite"
  enable_redis  = false
  enable_ssl    = false
  
  ebs_volume_size = 50
  
  tags = {
    Project = "alexander-simple"
  }
}

# Outputs
output "endpoint" {
  value = module.alexander.endpoint
}

output "access_key_id" {
  value     = module.alexander.access_key_id
  sensitive = true
}

output "secret_access_key" {
  value     = module.alexander.secret_access_key
  sensitive = true
}
