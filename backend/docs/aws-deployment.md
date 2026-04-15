# AWS Deployment Guide

This guide walks you through deploying the backend to AWS from scratch. No prior AWS experience is assumed. Every step includes the exact commands to run.

**Architecture recap:**

| Component | AWS Service | Why |
|---|---|---|
| Go API server | Elastic Beanstalk | Runs your compiled binary; AWS handles the server, load balancer, health checks |
| Python OR-Tools solver | Lambda | Runs only when called; scales to zero; no server to manage |
| PostgreSQL database | RDS | Managed DB: backups, patching, failover handled by AWS |
| API keys / secrets | Secrets Manager | Encrypted storage for credentials; no secrets in code or env files |
| Logs | CloudWatch Logs | Automatic with Elastic Beanstalk; searchable and alertable |
| HTTPS certificate | ACM (Certificate Manager) | Free SSL certificate; attached to the Elastic Beanstalk load balancer |

**Estimated monthly cost (MVP / low traffic):**
- RDS db.t3.micro (PostgreSQL): ~$15/month
- Elastic Beanstalk t3.micro: ~$8/month (just the EC2 instance)
- Lambda: effectively free at low volume (1M free requests/month)
- Secrets Manager: ~$0.40/secret/month
- Total: ~$25–35/month

---

## Table of Contents

1. [Prerequisites](#1-prerequisites)
2. [AWS Account Setup](#2-aws-account-setup)
3. [Create the RDS Database](#3-create-the-rds-database)
4. [Create Secrets in Secrets Manager](#4-create-secrets-in-secrets-manager)
5. [Deploy the Python Solver to Lambda](#5-deploy-the-python-solver-to-lambda)
6. [Set Up Elastic Beanstalk for the Go API](#6-set-up-elastic-beanstalk-for-the-go-api)
7. [Configure Environment Variables](#7-configure-environment-variables)
8. [Run Database Migrations](#8-run-database-migrations)
9. [Set Up HTTPS with ACM](#9-set-up-https-with-acm)
10. [Configure Stripe & GoCardless Webhooks](#10-configure-stripe--gocardless-webhooks)
11. [Set Up GitHub Actions CI/CD](#11-set-up-github-actions-cicd)
12. [Set Up CloudWatch Alarms](#12-set-up-cloudwatch-alarms)
13. [Pre-Launch Checklist](#13-pre-launch-checklist)
14. [Rollback Procedures](#14-rollback-procedures)

---

## 1. Prerequisites

Install the following tools on your Mac before starting:

```bash
# AWS CLI
brew install awscli

# Elastic Beanstalk CLI
brew install awsebcli

# Verify they installed
aws --version
eb --version
```

You also need:
- An **AWS account** (go to aws.amazon.com → Create account; you'll need a credit card)
- A **domain name** (e.g. from Namecheap, GoDaddy, or AWS Route 53)
- The backend code working locally first

---

## 2. AWS Account Setup

### 2.1 Create an IAM user for deployments

Never use your root AWS account for day-to-day operations. Create a dedicated deployment user.

1. Go to the **AWS Console** → search for **IAM** → **Users** → **Create user**
2. Name it `pt-scheduler-deploy`
3. Attach these managed policies directly:
   - `AdministratorAccess-AWSElasticBeanstalk`
   - `AmazonRDSFullAccess`
   - `AWSLambda_FullAccess`
   - `SecretsManagerReadWrite`
   - `CloudWatchFullAccess`
   - `AmazonS3FullAccess`
4. After creation, go to the user → **Security credentials** → **Create access key**
5. Choose **CLI** as the use case
6. Save the **Access key ID** and **Secret access key** — you only see the secret once

### 2.2 Configure the AWS CLI

```bash
aws configure
# Enter your Access key ID
# Enter your Secret access key
# Default region: eu-west-2   ← London (closest to UK users)
# Default output format: json
```

Verify it works:
```bash
aws sts get-caller-identity
# Should print your account ID and user name
```

### 2.3 Choose your region

This guide uses `eu-west-2` (London) for UK compliance and low latency. Every command that takes a `--region` flag will use this.

---

## 3. Create the RDS Database

### 3.1 Create a security group for RDS

The database should only accept connections from your Elastic Beanstalk EC2 instances, not from the open internet.

```bash
# First, find your default VPC ID
aws ec2 describe-vpcs \
  --filters "Name=is-default,Values=true" \
  --query "Vpcs[0].VpcId" \
  --output text \
  --region eu-west-2
# Note the output, e.g.: vpc-0abc12345

# Create a security group for RDS
aws ec2 create-security-group \
  --group-name pt-scheduler-rds-sg \
  --description "Allow PostgreSQL from Elastic Beanstalk" \
  --vpc-id vpc-0abc12345 \
  --region eu-west-2
# Note the GroupId output, e.g.: sg-0rds12345
```

We'll add the inbound rule after Elastic Beanstalk creates its security group (step 6).

### 3.2 Create the RDS instance

```bash
aws rds create-db-instance \
  --db-instance-identifier pt-scheduler-db \
  --db-instance-class db.t3.micro \
  --engine postgres \
  --engine-version 16 \
  --master-username ptadmin \
  --master-user-password "CHOOSE_A_STRONG_PASSWORD_HERE" \
  --allocated-storage 20 \
  --db-name pt_scheduler \
  --vpc-security-group-ids sg-0rds12345 \
  --no-publicly-accessible \
  --backup-retention-period 7 \
  --storage-encrypted \
  --region eu-west-2
```

> **This takes about 10 minutes.** Track progress:
> ```bash
> aws rds describe-db-instances \
>   --db-instance-identifier pt-scheduler-db \
>   --query "DBInstances[0].DBInstanceStatus" \
>   --region eu-west-2
> # Wait until it says: "available"
> ```

### 3.3 Get the database endpoint

```bash
aws rds describe-db-instances \
  --db-instance-identifier pt-scheduler-db \
  --query "DBInstances[0].Endpoint.Address" \
  --output text \
  --region eu-west-2
# e.g.: pt-scheduler-db.c1234abcd.eu-west-2.rds.amazonaws.com
```

Your `DATABASE_URL` will be:
```
postgres://ptadmin:YOUR_PASSWORD@pt-scheduler-db.c1234abcd.eu-west-2.rds.amazonaws.com:5432/pt_scheduler
```

---

## 4. Create Secrets in Secrets Manager

Store all sensitive values here instead of putting them in environment variable files.

```bash
# Create a single secret with all your API keys as JSON
aws secretsmanager create-secret \
  --name pt-scheduler/production \
  --description "PT Scheduler production credentials" \
  --secret-string '{
    "DATABASE_URL": "postgres://ptadmin:YOUR_PASSWORD@your-rds-endpoint.rds.amazonaws.com:5432/pt_scheduler",
    "JWT_SECRET": "run_openssl_rand_hex_32_and_paste_here",
    "STRIPE_SECRET_KEY": "sk_live_...",
    "STRIPE_WEBHOOK_SECRET": "whsec_...",
    "GOCARDLESS_ACCESS_TOKEN": "your_live_token",
    "GOCARDLESS_WEBHOOK_SECRET": "your_webhook_secret",
    "TWILIO_ACCOUNT_SID": "ACxxxx",
    "TWILIO_AUTH_TOKEN": "your_auth_token",
    "RESEND_API_KEY": "re_..."
  }' \
  --region eu-west-2
```

> **Note**: For the initial deploy, you can also set these as Elastic Beanstalk environment variables directly (step 7). Secrets Manager is the production best practice, but environment variables are simpler to get started.

---

## 5. Deploy the Python Solver to Lambda

### 5.1 Package the solver

OR-Tools is ~100MB, which exceeds the Lambda 50MB zip limit. We use a **Lambda Layer** to pre-package the dependencies separately.

```bash
# In the solver/ directory
cd solver

# Create a package directory for the layer
mkdir -p layer/python

# Install OR-Tools into the layer (must use Amazon Linux 2 compatible build)
pip install \
  --platform manylinux2014_x86_64 \
  --target layer/python \
  --implementation cp \
  --python-version 3.12 \
  --only-binary=:all: \
  ortools fastapi mangum

# Zip the layer
cd layer
zip -r ../solver-layer.zip python/
cd ..

# Zip just the function code (no dependencies)
zip -r solver-function.zip main.py solver.py
```

### 5.2 Create the Lambda Layer

```bash
aws lambda publish-layer-version \
  --layer-name pt-scheduler-solver-deps \
  --description "OR-Tools and FastAPI for PT Scheduler solver" \
  --zip-file fileb://solver-layer.zip \
  --compatible-runtimes python3.12 \
  --region eu-west-2
# Note the LayerVersionArn in the output
# e.g.: arn:aws:lambda:eu-west-2:123456789:layer:pt-scheduler-solver-deps:1
```

### 5.3 Create the Lambda function

```bash
# Create an IAM role for the Lambda function
aws iam create-role \
  --role-name pt-scheduler-lambda-role \
  --assume-role-policy-document '{
    "Version": "2012-10-17",
    "Statement": [{
      "Effect": "Allow",
      "Principal": {"Service": "lambda.amazonaws.com"},
      "Action": "sts:AssumeRole"
    }]
  }'

# Attach basic Lambda execution permissions (allows writing logs to CloudWatch)
aws iam attach-role-policy \
  --role-name pt-scheduler-lambda-role \
  --policy-arn arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole

# Wait a few seconds for the role to propagate, then create the function
aws lambda create-function \
  --function-name pt-scheduler-solver \
  --runtime python3.12 \
  --role arn:aws:iam::YOUR_ACCOUNT_ID:role/pt-scheduler-lambda-role \
  --handler main.handler \
  --zip-file fileb://solver-function.zip \
  --timeout 60 \
  --memory-size 512 \
  --layers arn:aws:lambda:eu-west-2:YOUR_ACCOUNT_ID:layer:pt-scheduler-solver-deps:1 \
  --region eu-west-2
```

### 5.4 Update `main.py` to work with Lambda

Lambda doesn't run a server — it invokes a handler function. Add **Mangum** as an adapter (already in the layer above):

Open `solver/main.py` and add this at the bottom:

```python
# Add this import at the top of solver/main.py:
from mangum import Mangum

# Add this at the bottom:
handler = Mangum(app)
```

Mangum wraps the FastAPI app so Lambda can invoke it as if it were a normal HTTP server.

### 5.5 Create a Function URL (so the Go API can call it)

```bash
aws lambda create-function-url-config \
  --function-name pt-scheduler-solver \
  --auth-type NONE \
  --region eu-west-2
# Note the FunctionUrl output
# e.g.: https://abc123.lambda-url.eu-west-2.on.aws/
```

Set `SOLVER_URL` to this URL in your Elastic Beanstalk environment variables (step 7).

### 5.6 Update the solver (future deployments)

```bash
cd solver
zip -r solver-function.zip main.py solver.py

aws lambda update-function-code \
  --function-name pt-scheduler-solver \
  --zip-file fileb://solver-function.zip \
  --region eu-west-2
```

---

## 6. Set Up Elastic Beanstalk for the Go API

Elastic Beanstalk runs your compiled Go binary. You give it a ZIP file containing the binary and a `Procfile`; AWS handles the EC2 instance, load balancer, health checks, and auto-restart on crash.

### 6.1 Create the Elastic Beanstalk application

```bash
# From the backend/ directory
eb init pt-scheduler \
  --platform "Go 1 running on 64bit Amazon Linux 2023" \
  --region eu-west-2
# When asked "Do you want to set up SSH?": Yes (useful for debugging)
# Create a new key pair and name it pt-scheduler-key
```

### 6.2 Create the `.ebextensions` configuration

Create the directory and a config file:

```bash
mkdir -p .ebextensions
```

Create `.ebextensions/01-env.config`:

```yaml
option_settings:
  aws:elasticbeanstalk:application:environment:
    PORT: 8080
    ENV: production
  aws:elasticbeanstalk:environment:proxy:
    ProxyServer: nginx
  aws:elasticbeanstalk:environment:proxy:staticfiles:
    /: /var/app/current
  aws:autoscaling:launchconfiguration:
    InstanceType: t3.micro
  aws:elasticbeanstalk:healthreporting:system:
    SystemType: enhanced
```

### 6.3 Create the `Procfile`

Create `Procfile` in the `backend/` root (no extension, capital P):

```
web: ./api
```

This tells Elastic Beanstalk to run the compiled binary named `api`.

### 6.4 Create a deployment script

Create `scripts/deploy.sh`:

```bash
#!/bin/bash
set -e

echo "Building Go binary for Linux..."
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o api ./cmd/api

echo "Creating deployment package..."
zip -r deploy.zip api Procfile .ebextensions/

echo "Deploying to Elastic Beanstalk..."
eb deploy pt-scheduler-prod

echo "Cleaning up..."
rm api deploy.zip

echo "Deploy complete!"
```

```bash
chmod +x scripts/deploy.sh
```

### 6.5 Create the Elastic Beanstalk environment

```bash
eb create pt-scheduler-prod \
  --cname pt-scheduler \
  --elb-type application \
  --instance-type t3.micro \
  --region eu-west-2
```

> **This takes about 5 minutes.** The EB CLI shows live progress.

### 6.6 Allow RDS to accept connections from Elastic Beanstalk

After EB creates the environment, find its security group:

```bash
# Get the EB environment's EC2 security group
aws ec2 describe-security-groups \
  --filters "Name=group-name,Values=awseb*" \
  --query "SecurityGroups[*].{Name:GroupName,ID:GroupId}" \
  --region eu-west-2
# Note the security group ID of your EB environment, e.g.: sg-0eb12345
```

Now allow that security group to connect to RDS on port 5432:

```bash
aws ec2 authorize-security-group-ingress \
  --group-id sg-0rds12345 \
  --protocol tcp \
  --port 5432 \
  --source-group sg-0eb12345 \
  --region eu-west-2
```

---

## 7. Configure Environment Variables

Set all environment variables in the Elastic Beanstalk environment. These are injected into the process at runtime.

```bash
eb setenv \
  DATABASE_URL="postgres://ptadmin:YOUR_PASSWORD@your-rds-endpoint.rds.amazonaws.com:5432/pt_scheduler" \
  JWT_SECRET="your_64_char_hex_string" \
  JWT_ACCESS_EXPIRY_MIN=15 \
  JWT_REFRESH_EXPIRY_DAYS=7 \
  PASSWORD_RESET_EXPIRY_MIN=60 \
  CORS_ALLOWED_ORIGINS="https://yourfrontend.com" \
  STRIPE_SECRET_KEY="sk_live_..." \
  STRIPE_WEBHOOK_SECRET="whsec_..." \
  GOCARDLESS_ACCESS_TOKEN="your_live_token" \
  GOCARDLESS_WEBHOOK_SECRET="your_webhook_secret" \
  GOCARDLESS_ENV="live" \
  TWILIO_ACCOUNT_SID="ACxxxxxxxx" \
  TWILIO_AUTH_TOKEN="your_auth_token" \
  TWILIO_FROM_NUMBER="+441234567890" \
  MESSAGING_CHANNEL="sms" \
  RESEND_API_KEY="re_..." \
  RESEND_FROM_ADDRESS="PT Scheduler <notifications@yourptapp.com>" \
  SOLVER_URL="https://abc123.lambda-url.eu-west-2.on.aws" \
  SOLVER_TIMEOUT_SECONDS=30 \
  ENV="production" \
  PORT=8080
```

> **Tip**: You can also set these in the AWS Console: Elastic Beanstalk → your environment → Configuration → Updates, monitoring, and logging → Environment properties.

Verify the variables are set:
```bash
eb printenv
```

---

## 8. Run Database Migrations

Migrations need to run from somewhere that can reach your RDS instance. Since RDS is not publicly accessible, you have two options.

### Option A: Bastion EC2 instance (one-time setup)

A bastion is a small, temporary EC2 instance in the same VPC as RDS, used only to run migrations.

```bash
# Launch a tiny bastion instance (Amazon Linux 2023)
aws ec2 run-instances \
  --image-id ami-0eb260c4d5475b901 \
  --instance-type t3.nano \
  --key-name pt-scheduler-key \
  --security-group-ids sg-0rds12345 \
  --region eu-west-2 \
  --query "Instances[0].InstanceId" \
  --output text
# Note the instance ID, e.g.: i-0abc12345

# Get the public IP
aws ec2 describe-instances \
  --instance-ids i-0abc12345 \
  --query "Reservations[0].Instances[0].PublicIpAddress" \
  --output text \
  --region eu-west-2

# SSH into it
ssh -i ~/.ssh/pt-scheduler-key.pem ec2-user@YOUR_PUBLIC_IP

# On the bastion: install the migrate CLI
curl -L https://github.com/golang-migrate/migrate/releases/download/v4.17.0/migrate.linux-amd64.tar.gz | tar xvz
sudo mv migrate /usr/local/bin/

# Upload your migrations folder to the bastion
# (run this on your local machine in another terminal)
scp -i ~/.ssh/pt-scheduler-key.pem -r ./migrations ec2-user@YOUR_PUBLIC_IP:/home/ec2-user/

# Back on the bastion: run migrations
migrate \
  -path /home/ec2-user/migrations \
  -database "postgres://ptadmin:YOUR_PASSWORD@your-rds-endpoint.rds.amazonaws.com:5432/pt_scheduler?sslmode=require" \
  up

# Terminate the bastion when done
exit
aws ec2 terminate-instances --instance-ids i-0abc12345 --region eu-west-2
```

### Option B: Run migrations from GitHub Actions (CI/CD)

The GitHub Actions pipeline (step 11) can run migrations automatically on deploy. This is the better long-term approach. It works because GitHub Actions can connect to RDS if you temporarily add the runner's IP to the RDS security group, or by using an AWS Systems Manager Session Manager connection.

For simplicity at MVP, Option A is fine for the first deploy.

---

## 9. Set Up HTTPS with ACM

Your API must be served over HTTPS (Stripe webhooks require it, and browsers block mixed content).

### 9.1 Request an SSL certificate

```bash
aws acm request-certificate \
  --domain-name api.yourptapp.com \
  --validation-method DNS \
  --region eu-west-2
# Note the CertificateArn output
```

### 9.2 Validate the certificate via DNS

1. Go to **AWS Console** → **ACM** → your certificate → **Create records in Route 53** (if using Route 53) or copy the CNAME records and add them to your DNS provider manually
2. Wait for status to change from `PENDING_VALIDATION` to `ISSUED` (5–30 minutes)

### 9.3 Attach the certificate to the load balancer

```bash
eb setenv EB_LOAD_BALANCER_SCHEME=internet-facing

# Or configure via the EB console:
# Elastic Beanstalk → pt-scheduler-prod → Configuration →
# Load balancer → Add listener → Port 443 → Protocol HTTPS →
# SSL certificate → select your ACM certificate
```

Then update your CORS and webhook URLs to use `https://api.yourptapp.com`.

### 9.4 Point your domain to Elastic Beanstalk

In your DNS provider (or Route 53), add a CNAME record:
```
api.yourptapp.com → pt-scheduler.eu-west-2.elasticbeanstalk.com
```

---

## 10. Configure Stripe & GoCardless Webhooks

Once you have a live HTTPS URL, update your webhook endpoints.

### Stripe

1. Go to **Stripe Dashboard** → **Developers** → **Webhooks** → **Add endpoint**
2. Endpoint URL: `https://api.yourptapp.com/api/v1/webhooks/stripe`
3. Events to listen for:
   - `payment_intent.succeeded`
   - `payment_intent.payment_failed`
4. Copy the **Signing secret** (`whsec_...`) and update `STRIPE_WEBHOOK_SECRET` in EB:
   ```bash
   eb setenv STRIPE_WEBHOOK_SECRET="whsec_your_new_secret"
   ```

### GoCardless

1. Go to **GoCardless Dashboard** → **Developers** → **Webhooks** → **Create endpoint**
2. Endpoint URL: `https://api.yourptapp.com/api/v1/webhooks/gocardless`
3. Copy the secret and update `GOCARDLESS_WEBHOOK_SECRET`:
   ```bash
   eb setenv GOCARDLESS_WEBHOOK_SECRET="your_new_secret"
   ```

### Twilio

1. Go to **Twilio Console** → **Phone Numbers** → your number → **Messaging**
2. Set "A message comes in" → Webhook → `https://api.yourptapp.com/api/v1/webhooks/twilio`

---

## 11. Set Up GitHub Actions CI/CD

This automates the whole process: push to `main` → tests run → binary built → deployed to Elastic Beanstalk.

### 11.1 Add GitHub Secrets

In your GitHub repository: **Settings** → **Secrets and variables** → **Actions** → **New repository secret**

Add these secrets:
| Secret name | Value |
|---|---|
| `AWS_ACCESS_KEY_ID` | Your IAM user's access key ID |
| `AWS_SECRET_ACCESS_KEY` | Your IAM user's secret access key |
| `AWS_REGION` | `eu-west-2` |
| `EB_APP_NAME` | `pt-scheduler` |
| `EB_ENV_NAME` | `pt-scheduler-prod` |
| `DATABASE_URL` | Your full RDS connection string (for running migrations) |

### 11.2 Create the workflow file

Create `.github/workflows/deploy.yml`:

```yaml
name: CI/CD

on:
  push:
    branches: [main]
  pull_request:
    branches: [main]

jobs:
  test:
    name: Test
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - name: Set up Go
        uses: actions/setup-go@v5
        with:
          go-version: '1.22'
          cache: true

      - name: Verify dependencies
        run: go mod verify

      - name: Run vet
        run: go vet ./...

      - name: Run tests
        run: go test -race ./...

      - name: Build
        run: GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o /dev/null ./cmd/api

  deploy:
    name: Deploy
    runs-on: ubuntu-latest
    needs: test
    if: github.ref == 'refs/heads/main' && github.event_name == 'push'

    steps:
      - uses: actions/checkout@v4

      - name: Set up Go
        uses: actions/setup-go@v5
        with:
          go-version: '1.22'
          cache: true

      - name: Build Linux binary
        run: GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o api ./cmd/api

      - name: Create deployment package
        run: zip -r deploy.zip api Procfile .ebextensions/

      - name: Configure AWS credentials
        uses: aws-actions/configure-aws-credentials@v4
        with:
          aws-access-key-id: ${{ secrets.AWS_ACCESS_KEY_ID }}
          aws-secret-access-key: ${{ secrets.AWS_SECRET_ACCESS_KEY }}
          aws-region: ${{ secrets.AWS_REGION }}

      - name: Run database migrations
        run: |
          curl -L https://github.com/golang-migrate/migrate/releases/download/v4.17.0/migrate.linux-amd64.tar.gz | tar xvz
          ./migrate -path ./migrations -database "${{ secrets.DATABASE_URL }}?sslmode=require" up

      - name: Deploy to Elastic Beanstalk
        uses: einaregilsson/beanstalk-deploy@v22
        with:
          aws_access_key: ${{ secrets.AWS_ACCESS_KEY_ID }}
          aws_secret_key: ${{ secrets.AWS_SECRET_ACCESS_KEY }}
          application_name: ${{ secrets.EB_APP_NAME }}
          environment_name: ${{ secrets.EB_ENV_NAME }}
          region: ${{ secrets.AWS_REGION }}
          version_label: ${{ github.sha }}
          deployment_package: deploy.zip
```

> **Security note on migrations**: Running migrations from GitHub Actions requires the runner to reach your RDS. Either:
> - Make RDS temporarily public (not recommended for production)
> - Add the runner's IP to the RDS security group dynamically (more complex)
> - Use AWS Systems Manager for a private connection
>
> For a university project MVP, running migrations manually via the bastion (step 8) before deploying is perfectly fine.

### 11.3 Simplified workflow (skip auto-migrations)

If you want to handle migrations manually and only automate the deploy:

```yaml
name: Deploy

on:
  push:
    branches: [main]

jobs:
  deploy:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.22'
          cache: true
      - run: go test ./...
      - run: GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o api ./cmd/api
      - run: zip deploy.zip api Procfile .ebextensions/
      - uses: aws-actions/configure-aws-credentials@v4
        with:
          aws-access-key-id: ${{ secrets.AWS_ACCESS_KEY_ID }}
          aws-secret-access-key: ${{ secrets.AWS_SECRET_ACCESS_KEY }}
          aws-region: eu-west-2
      - uses: einaregilsson/beanstalk-deploy@v22
        with:
          aws_access_key: ${{ secrets.AWS_ACCESS_KEY_ID }}
          aws_secret_key: ${{ secrets.AWS_SECRET_ACCESS_KEY }}
          application_name: pt-scheduler
          environment_name: pt-scheduler-prod
          region: eu-west-2
          version_label: ${{ github.sha }}
          deployment_package: deploy.zip
```

---

## 12. Set Up CloudWatch Alarms

These alert you when things go wrong.

### 12.1 High 5xx error rate

```bash
aws cloudwatch put-metric-alarm \
  --alarm-name "pt-scheduler-high-5xx" \
  --alarm-description "API error rate too high" \
  --metric-name "HTTPCode_Target_5XX_Count" \
  --namespace "AWS/ApplicationELB" \
  --statistic Sum \
  --period 300 \
  --evaluation-periods 2 \
  --threshold 10 \
  --comparison-operator GreaterThanThreshold \
  --alarm-actions arn:aws:sns:eu-west-2:YOUR_ACCOUNT_ID:pt-scheduler-alerts \
  --region eu-west-2
```

### 12.2 Set up SNS for email alerts (prerequisite for alarms)

```bash
# Create an SNS topic
aws sns create-topic \
  --name pt-scheduler-alerts \
  --region eu-west-2
# Note the TopicArn

# Subscribe your email to it
aws sns subscribe \
  --topic-arn arn:aws:sns:eu-west-2:YOUR_ACCOUNT_ID:pt-scheduler-alerts \
  --protocol email \
  --notification-endpoint your@email.com \
  --region eu-west-2
# You'll get a confirmation email — click the link
```

### 12.3 Database CPU alarm

```bash
aws cloudwatch put-metric-alarm \
  --alarm-name "pt-scheduler-db-cpu" \
  --alarm-description "RDS CPU too high" \
  --metric-name CPUUtilization \
  --namespace AWS/RDS \
  --dimensions Name=DBInstanceIdentifier,Value=pt-scheduler-db \
  --statistic Average \
  --period 300 \
  --evaluation-periods 3 \
  --threshold 80 \
  --comparison-operator GreaterThanThreshold \
  --alarm-actions arn:aws:sns:eu-west-2:YOUR_ACCOUNT_ID:pt-scheduler-alerts \
  --region eu-west-2
```

### 12.4 Set up UptimeRobot (free uptime monitoring)

1. Go to [uptimerobot.com](https://uptimerobot.com) → create a free account
2. Add new monitor → HTTP(s) → `https://api.yourptapp.com/healthz`
3. Check interval: 5 minutes
4. Alert when down → add your email

This gives you a public status page and SMS/email alerts when the server goes down.

---

## 13. Pre-Launch Checklist

Work through this before going live:

### Infrastructure
- [ ] RDS automated backups enabled (we set 7-day retention above)
- [ ] RDS is not publicly accessible (confirmed during creation)
- [ ] Elastic Beanstalk enhanced health reporting enabled
- [ ] HTTPS working (`curl https://api.yourptapp.com/healthz` returns 200)
- [ ] CloudWatch alarms created and SNS email confirmed
- [ ] UptimeRobot monitoring active

### Application
- [ ] All migrations applied (`/readyz` returns 200)
- [ ] `GOCARDLESS_ENV=live` (not sandbox)
- [ ] Stripe live keys in use (not test keys)
- [ ] Stripe webhook endpoint points to production URL
- [ ] GoCardless webhook endpoint points to production URL
- [ ] Twilio SMS webhook points to production URL
- [ ] `CORS_ALLOWED_ORIGINS` set to your production frontend URL only (not localhost)
- [ ] `ENV=production` (enables JSON log format)

### Security
- [ ] `.env` file is in `.gitignore` (never committed)
- [ ] JWT secret is at least 32 random bytes
- [ ] RDS password is strong (12+ chars, mixed case, numbers, symbols)
- [ ] IAM user for deployment has minimal necessary permissions (not AdministratorAccess)

### Testing
- [ ] `GET /healthz` → 200
- [ ] `GET /readyz` → 200 (confirms DB connection)
- [ ] `POST /api/v1/auth/register` works end-to-end
- [ ] Password reset email arrives
- [ ] Stripe test payment succeeds (using Stripe test card `4242 4242 4242 4242`)

---

## 14. Rollback Procedures

### Roll back the API (Elastic Beanstalk)

Each deploy creates a version in EB. To go back:

```bash
# List available versions
aws elasticbeanstalk describe-application-versions \
  --application-name pt-scheduler \
  --region eu-west-2 \
  --query "ApplicationVersions[*].{Label:VersionLabel,Date:DateCreated}" \
  --output table

# Roll back to a specific version (use the git SHA as the label)
aws elasticbeanstalk update-environment \
  --environment-name pt-scheduler-prod \
  --version-label PREVIOUS_GIT_SHA \
  --region eu-west-2
```

### Roll back a database migration

```bash
# Roll back the most recent migration
go run ./cmd/migrate down 1

# Or roll back to a specific version
go run ./cmd/migrate down 2  # rolls back 2 migrations
```

> **Important**: Never roll back a migration if data has already been written to the new columns/tables. Always test migrations on a staging database first.

### Emergency: restore from RDS backup

```bash
# List available automated backups
aws rds describe-db-snapshots \
  --db-instance-identifier pt-scheduler-db \
  --snapshot-type automated \
  --region eu-west-2

# Restore to a point in time (creates a NEW RDS instance)
aws rds restore-db-instance-to-point-in-time \
  --source-db-instance-identifier pt-scheduler-db \
  --target-db-instance-identifier pt-scheduler-db-restored \
  --restore-time 2025-01-15T12:00:00Z \
  --region eu-west-2
# Then update DATABASE_URL to point at the restored instance
```

---

## Quick Reference: Day-to-Day Operations

```bash
# Check environment health
eb status

# View live logs
eb logs --all

# View logs in CloudWatch (last 100 lines)
aws logs tail /aws/elasticbeanstalk/pt-scheduler-prod/var/log/web.stdout.log \
  --follow \
  --region eu-west-2

# SSH into the running instance (for debugging)
eb ssh

# Deploy a new version manually
./scripts/deploy.sh

# Run migrations manually
go run ./cmd/migrate up

# Check database connections
aws rds describe-db-instances \
  --db-instance-identifier pt-scheduler-db \
  --query "DBInstances[0].DBInstanceStatus" \
  --region eu-west-2
```
