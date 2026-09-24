# Throwaway sandbox for tofu-drift: about fifteen covered resource types,
# sized to cost pennies an hour. See README.md before applying.

terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
    archive = {
      source  = "hashicorp/archive"
      version = "~> 2.0"
    }
  }
}

variable "profile" {
  description = "AWS CLI profile for a throwaway account. No default on purpose."
  type        = string
}

variable "region" {
  type    = string
  default = "us-east-1"
}

variable "name_prefix" {
  description = "Prefix for every name, and the value of the sandbox tag."
  type        = string
  default     = "tdsbx"

  validation {
    condition     = can(regex("^[a-z][a-z0-9-]{1,20}$", var.name_prefix))
    error_message = "name_prefix must be 2-21 lowercase letters, digits or dashes (it is used in an S3 bucket name)."
  }
}

provider "aws" {
  profile = var.profile
  region  = var.region

  # Everything carries the sandbox tag, so teardown can find strays.
  default_tags {
    tags = { "tofu-drift-sandbox" = var.name_prefix }
  }
}

data "aws_caller_identity" "me" {}

data "aws_availability_zones" "up" {
  state = "available"
}

data "aws_ssm_parameter" "al2023_arm64" {
  name = "/aws/service/ami-amazon-linux-latest/al2023-ami-kernel-default-arm64"
}

locals {
  azs = slice(data.aws_availability_zones.up.names, 0, 2)
}

# --- Network: VPC, subnets, route table, security group ---

resource "aws_vpc" "main" {
  cidr_block = "10.42.0.0/16"
  tags       = { Name = "${var.name_prefix}-vpc" }
}

# Free, and a public NAT gateway (planted by make-mess.sh) refuses a VPC without one.
resource "aws_internet_gateway" "main" {
  vpc_id = aws_vpc.main.id
  tags   = { Name = "${var.name_prefix}-igw" }
}

resource "aws_subnet" "main" {
  count             = 2
  vpc_id            = aws_vpc.main.id
  cidr_block        = cidrsubnet(aws_vpc.main.cidr_block, 8, count.index)
  availability_zone = local.azs[count.index]
  tags              = { Name = "${var.name_prefix}-subnet-${count.index}" }
}

resource "aws_route_table" "main" {
  vpc_id = aws_vpc.main.id
  route {
    cidr_block = "0.0.0.0/0"
    gateway_id = aws_internet_gateway.main.id
  }
  tags = { Name = "${var.name_prefix}-rt" }
}

resource "aws_route_table_association" "main" {
  count          = 2
  subnet_id      = aws_subnet.main[count.index].id
  route_table_id = aws_route_table.main.id
}

resource "aws_security_group" "main" {
  name        = "${var.name_prefix}-sg"
  description = "tofu-drift sandbox"
  vpc_id      = aws_vpc.main.id

  ingress {
    description = "HTTP inside the VPC"
    from_port   = 80
    to_port     = 80
    protocol    = "tcp"
    cidr_blocks = [aws_vpc.main.cidr_block]
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

  tags = { Name = "${var.name_prefix}-sg" }
}

# --- Compute: a stopped t4g.nano, a later-attached volume, a snapshot, an AMI ---

resource "aws_instance" "main" {
  ami                         = data.aws_ssm_parameter.al2023_arm64.value
  instance_type               = "t4g.nano"
  subnet_id                   = aws_subnet.main[0].id
  vpc_security_group_ids      = [aws_security_group.main.id]
  associate_public_ip_address = false

  root_block_device {
    volume_type = "gp3"
    volume_size = 8
  }

  tags = { Name = "${var.name_prefix}-instance" }
}

# Stopped: only its EBS is billed.
resource "aws_ec2_instance_state" "main" {
  instance_id = aws_instance.main.id
  state       = "stopped"
}

resource "aws_ebs_volume" "data" {
  availability_zone = local.azs[0]
  type              = "gp3"
  size              = 1
  tags              = { Name = "${var.name_prefix}-data" }
}

resource "aws_volume_attachment" "data" {
  device_name = "/dev/sdf"
  volume_id   = aws_ebs_volume.data.id
  instance_id = aws_instance.main.id
}

resource "aws_ebs_snapshot" "data" {
  volume_id = aws_ebs_volume.data.id
  tags      = { Name = "${var.name_prefix}-data-snap" }
}

# Registered from the 1 GB snapshot; never launched, so it reads as Idle.
resource "aws_ami" "main" {
  name                = "${var.name_prefix}-ami"
  architecture        = "arm64"
  virtualization_type = "hvm"
  root_device_name    = "/dev/xvda"
  ena_support         = true

  ebs_block_device {
    device_name = "/dev/xvda"
    snapshot_id = aws_ebs_snapshot.data.id
    volume_size = 1
    volume_type = "gp3"
  }
}

resource "aws_launch_template" "main" {
  name_prefix   = "${var.name_prefix}-lt-"
  image_id      = data.aws_ssm_parameter.al2023_arm64.value
  instance_type = "t4g.nano"
}

# Zero capacity: exists, costs nothing.
resource "aws_autoscaling_group" "main" {
  name                = "${var.name_prefix}-asg"
  min_size            = 0
  max_size            = 0
  desired_capacity    = 0
  vpc_zone_identifier = aws_subnet.main[*].id

  launch_template {
    id      = aws_launch_template.main.id
    version = "$Latest"
  }

  # default_tags does not reach ASGs.
  tag {
    key                 = "tofu-drift-sandbox"
    value               = var.name_prefix
    propagate_at_launch = true
  }
}

resource "aws_ecs_cluster" "main" {
  name = "${var.name_prefix}-ecs"
}

# --- Load balancer: internal ALB with one empty target group (Idle) ---

resource "aws_lb" "main" {
  name               = "${var.name_prefix}-alb"
  internal           = true
  load_balancer_type = "application"
  subnets            = aws_subnet.main[*].id
  security_groups    = [aws_security_group.main.id]
}

resource "aws_lb_target_group" "main" {
  name     = "${var.name_prefix}-tg"
  port     = 80
  protocol = "HTTP"
  vpc_id   = aws_vpc.main.id
}

resource "aws_lb_listener" "main" {
  load_balancer_arn = aws_lb.main.arn
  port              = 80
  protocol          = "HTTP"

  default_action {
    type             = "forward"
    target_group_arn = aws_lb_target_group.main.arn
  }
}

# --- Data: S3, DynamoDB ---

resource "aws_s3_bucket" "main" {
  bucket_prefix = "${var.name_prefix}-"
  force_destroy = true
}

resource "aws_dynamodb_table" "main" {
  name         = "${var.name_prefix}-table"
  billing_mode = "PAY_PER_REQUEST"
  hash_key     = "id"

  attribute {
    name = "id"
    type = "S"
  }
}

# --- Serverless and plumbing: Lambda, log groups, IAM, Route53 ---

resource "aws_iam_role" "lambda" {
  name        = "${var.name_prefix}-lambda"
  description = "tofu-drift sandbox Lambda role"
  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Action    = "sts:AssumeRole"
      Principal = { Service = "lambda.amazonaws.com" }
    }]
  })
}

resource "aws_iam_user" "main" {
  name          = "${var.name_prefix}-user"
  force_destroy = true
}

data "archive_file" "lambda" {
  type        = "zip"
  output_path = "${path.module}/.terraform/lambda.zip"
  source {
    filename = "index.py"
    content  = "def handler(event, context):\n    return 'ok'\n"
  }
}

resource "aws_cloudwatch_log_group" "lambda" {
  name              = "/aws/lambda/${var.name_prefix}-fn"
  retention_in_days = 1
}

resource "aws_lambda_function" "main" {
  function_name    = "${var.name_prefix}-fn"
  role             = aws_iam_role.lambda.arn
  runtime          = "python3.12"
  architectures    = ["arm64"]
  handler          = "index.handler"
  memory_size      = 128
  timeout          = 3
  filename         = data.archive_file.lambda.output_path
  source_code_hash = data.archive_file.lambda.output_base64sha256
  depends_on       = [aws_cloudwatch_log_group.lambda]
}

# No retention on purpose: the classic leak.
resource "aws_cloudwatch_log_group" "app" {
  name = "${var.name_prefix}-app"
}

resource "aws_route53_zone" "main" {
  name = "${var.name_prefix}.sandbox.internal"
  vpc {
    vpc_id = aws_vpc.main.id
  }
}

# --- Outputs read by make-mess.sh, teardown.sh and capture-fixtures.sh ---

output "account_id" {
  value = data.aws_caller_identity.me.account_id
}

output "region" {
  value = var.region
}

output "name_prefix" {
  value = var.name_prefix
}

output "subnet_id" {
  value = aws_subnet.main[0].id
}

output "availability_zone" {
  value = local.azs[0]
}

output "instance_id" {
  value = aws_instance.main.id
}

output "security_group_id" {
  value = aws_security_group.main.id
}

output "lambda_name" {
  value = aws_lambda_function.main.function_name
}

output "log_group_name" {
  value = aws_cloudwatch_log_group.app.name
}

output "iam_role_name" {
  value = aws_iam_role.lambda.name
}
