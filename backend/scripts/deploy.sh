#!/bin/bash
set -e

echo "Building Go binary for Linux..."
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o server ./cmd/api

echo "Creating deployment package..."
zip deploy.zip server Procfile .ebextensions/01-env.config

echo "Deploying to Elastic Beanstalk..."
eb deploy pt-scheduler-prod

echo "Cleaning up..."
rm server deploy.zip

echo "Deploy complete!"
