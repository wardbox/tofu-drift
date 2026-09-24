# Throwaway stack for exercising tofu-drift against a real account.
# Region and credentials come from AWS_REGION / AWS_PROFILE.
# Cheap types only: no RDS, EKS, ElastiCache, or load balancers.

terraform {
  required_providers {
    aws     = { source = "hashicorp/aws", version = "~> 6.0" }
    archive = { source = "hashicorp/archive", version = "~> 2.0" }
  }
}

provider "aws" {
  default_tags {
    tags = { Project = "tofu-drift-sandbox" }
  }
}

locals {
  name = "tofu-drift-sandbox"
}

data "aws_caller_identity" "me" {}

data "aws_availability_zones" "up" {
  state = "available"
}

data "aws_ssm_parameter" "al2023" {
  name = "/aws/service/ami-amazon-linux-latest/al2023-ami-kernel-default-arm64"
}

# Plumbing

resource "aws_vpc" "main" {
  cidr_block = "10.42.0.0/16"
  tags       = { Name = local.name }
}

resource "aws_subnet" "public" {
  vpc_id            = aws_vpc.main.id
  cidr_block        = "10.42.1.0/24"
  availability_zone = data.aws_availability_zones.up.names[0]
  tags              = { Name = "${local.name}-public" }
}

resource "aws_internet_gateway" "main" {
  vpc_id = aws_vpc.main.id
  tags   = { Name = local.name }
}

resource "aws_route_table" "public" {
  vpc_id = aws_vpc.main.id
  route {
    cidr_block = "0.0.0.0/0"
    gateway_id = aws_internet_gateway.main.id
  }
  tags = { Name = "${local.name}-public" }
}

resource "aws_route_table_association" "public" {
  subnet_id      = aws_subnet.public.id
  route_table_id = aws_route_table.public.id
}

# No ingress: make-mess adds a rule out of band.
resource "aws_security_group" "app" {
  name        = "${local.name}-app"
  description = "tofu-drift sandbox app"
  vpc_id      = aws_vpc.main.id
  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }
  tags = { Name = "${local.name}-app" }
}

# Compute and storage

resource "aws_instance" "app" {
  ami                    = data.aws_ssm_parameter.al2023.value
  instance_type          = "t4g.nano"
  subnet_id              = aws_subnet.public.id
  vpc_security_group_ids = [aws_security_group.app.id]
  tags                   = { Name = "${local.name}-app" }
}

resource "aws_eip" "app" {
  instance = aws_instance.app.id
  tags     = { Name = "${local.name}-app" }
  depends_on = [aws_internet_gateway.main]
}

resource "aws_ebs_volume" "data" {
  availability_zone = aws_instance.app.availability_zone
  size              = 1
  type              = "gp3"
  tags              = { Name = "${local.name}-data" }
}

resource "aws_volume_attachment" "data" {
  device_name = "/dev/sdf"
  volume_id   = aws_ebs_volume.data.id
  instance_id = aws_instance.app.id
}

resource "aws_ebs_snapshot" "data" {
  volume_id = aws_ebs_volume.data.id
  tags      = { Name = "${local.name}-data" }
}

resource "aws_ecs_cluster" "main" {
  name = local.name
}

# Data

resource "aws_s3_bucket" "main" {
  bucket        = "${local.name}-${data.aws_caller_identity.me.account_id}"
  force_destroy = true
}

resource "aws_dynamodb_table" "main" {
  name         = local.name
  billing_mode = "PAY_PER_REQUEST"
  hash_key     = "id"
  attribute {
    name = "id"
    type = "S"
  }
}

# Identity, functions, logs, DNS

resource "aws_iam_role" "fn" {
  name = "${local.name}-fn"
  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Action    = "sts:AssumeRole"
      Principal = { Service = "lambda.amazonaws.com" }
    }]
  })
}

resource "aws_iam_user" "ci" {
  name = "${local.name}-ci"
}

data "archive_file" "fn" {
  type        = "zip"
  output_path = "${path.module}/.build/fn.zip"
  source {
    filename = "index.mjs"
    content  = "export const handler = async () => 'ok';"
  }
}

resource "aws_lambda_function" "fn" {
  function_name    = local.name
  role             = aws_iam_role.fn.arn
  runtime          = "nodejs22.x"
  handler          = "index.handler"
  architectures    = ["arm64"]
  filename         = data.archive_file.fn.output_path
  source_code_hash = data.archive_file.fn.output_base64sha256
}

resource "aws_cloudwatch_log_group" "app" {
  name              = "/${local.name}/app"
  retention_in_days = 7
}

# Private zones are free to create and deleted within 12h are not billed.
resource "aws_route53_zone" "internal" {
  name = "sandbox.internal"
  vpc {
    vpc_id = aws_vpc.main.id
  }
}

output "vpc_id" { value = aws_vpc.main.id }
output "subnet_id" { value = aws_subnet.public.id }
output "instance_id" { value = aws_instance.app.id }
output "security_group_id" { value = aws_security_group.app.id }
output "bucket" { value = aws_s3_bucket.main.id }
output "log_group" { value = aws_cloudwatch_log_group.app.name }
