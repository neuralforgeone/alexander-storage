#!/bin/bash
# Alexander Storage EC2 User Data Script
set -e

# Configuration from Terraform
NAME="${name}"
ENVIRONMENT="${environment}"
REGION="${region}"
DATABASE_TYPE="${database_type}"
POSTGRES_HOST="${postgres_host}"
POSTGRES_PORT="${postgres_port}"
POSTGRES_DATABASE="${postgres_database}"
POSTGRES_USERNAME="${postgres_username}"
POSTGRES_PASSWORD="${postgres_password}"
ENABLE_REDIS="${enable_redis}"
REDIS_HOST="${redis_host}"
REDIS_PORT="${redis_port}"
STORAGE_PATH="${storage_path}"
ALEXANDER_VERSION="${alexander_version}"

# Update system
yum update -y
yum install -y docker jq aws-cli

# Start Docker
systemctl start docker
systemctl enable docker

# Create directories
mkdir -p "$STORAGE_PATH"
mkdir -p /etc/alexander

# Get instance metadata
INSTANCE_ID=$(curl -s http://169.254.169.254/latest/meta-data/instance-id)
PRIVATE_IP=$(curl -s http://169.254.169.254/latest/meta-data/local-ipv4)

# Get admin credentials from SSM
ADMIN_ACCESS_KEY=$(aws ssm get-parameter --name "/$NAME/$ENVIRONMENT/admin-access-key" --with-decryption --region $REGION --query 'Parameter.Value' --output text)
ADMIN_SECRET_KEY=$(aws ssm get-parameter --name "/$NAME/$ENVIRONMENT/admin-secret-key" --with-decryption --region $REGION --query 'Parameter.Value' --output text)

# Generate configuration
cat > /etc/alexander/config.yaml <<EOF
server:
  address: "0.0.0.0:8080"
  read_timeout: 30s
  write_timeout: 30s
  shutdown_timeout: 10s

database:
  type: "$DATABASE_TYPE"
%{ if database_type == "postgresql" ~}
  postgres:
    host: "$POSTGRES_HOST"
    port: $POSTGRES_PORT
    database: "$POSTGRES_DATABASE"
    username: "$POSTGRES_USERNAME"
    password: "$POSTGRES_PASSWORD"
    sslmode: "require"
%{ else ~}
  sqlite:
    path: "/data/alexander.db"
%{ endif ~}

storage:
  backend: "filesystem"
  filesystem:
    path: "$STORAGE_PATH"

%{ if enable_redis ~}
cache:
  type: "redis"
  redis:
    address: "$REDIS_HOST:$REDIS_PORT"
%{ else ~}
cache:
  type: "memory"
%{ endif ~}

cluster:
  enabled: true
  node_id: "$INSTANCE_ID"
  advertise_address: "$PRIVATE_IP:9090"
  grpc_port: 9090

logging:
  level: "info"
  format: "json"

metrics:
  enabled: true
  address: "0.0.0.0:9100"

auth:
  bootstrap_access_key: "$ADMIN_ACCESS_KEY"
  bootstrap_secret_key: "$ADMIN_SECRET_KEY"
EOF

# Pull and run Alexander container
docker pull ghcr.io/neuralforgeone/alexander-storage:$ALEXANDER_VERSION

docker run -d \
  --name alexander \
  --restart unless-stopped \
  -p 8080:8080 \
  -p 9090:9090 \
  -p 9100:9100 \
  -v /etc/alexander:/etc/alexander:ro \
  -v $STORAGE_PATH:$STORAGE_PATH \
  ghcr.io/neuralforgeone/alexander-storage:$ALEXANDER_VERSION \
  -config /etc/alexander/config.yaml

# Wait for service to be healthy
echo "Waiting for Alexander to start..."
for i in {1..30}; do
  if curl -s http://localhost:8080/health | grep -q "ok"; then
    echo "Alexander started successfully"
    break
  fi
  sleep 2
done

# Setup CloudWatch logging
cat > /etc/awslogs/awslogs.conf <<EOF
[general]
state_file = /var/lib/awslogs/agent-state

[/var/log/messages]
datetime_format = %b %d %H:%M:%S
file = /var/log/messages
buffer_duration = 5000
log_stream_name = {instance_id}/messages
initial_position = start_of_file
log_group_name = /alexander/$ENVIRONMENT

[alexander]
datetime_format = %Y-%m-%dT%H:%M:%S
file = /var/lib/docker/containers/*/alexander*.log
buffer_duration = 5000
log_stream_name = {instance_id}/alexander
initial_position = start_of_file
log_group_name = /alexander/$ENVIRONMENT
EOF

# Install and start CloudWatch agent
yum install -y awslogs
systemctl start awslogsd
systemctl enable awslogsd

echo "Alexander Storage setup complete!"
