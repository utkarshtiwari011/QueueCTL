#!/bin/bash

# Seeding script for QueueCTL
# Enqueues a variety of mock jobs to test polling, backoffs, panics, and DLQ routing.

echo "Seeding QueueCTL database with mock jobs..."

# 1. Enqueue standard successful email jobs
queuectl enqueue email '{"to": "alice@example.com", "body": "Welcome Alice!"}'
queuectl enqueue email '{"to": "bob@example.com", "body": "Welcome Bob!"}'

# 2. Enqueue image resizing job
queuectl enqueue image_resize '{"image_path": "/var/tmp/avatar.png", "width": 250, "height": 250}'

# 3. Enqueue a job that will fail and trigger exponential backoff retries
queuectl enqueue error_demo '{"target_api": "https://api.external.com/sync", "timeout": 30}'

# 4. Enqueue a job that will cause a panic, verifying worker pool resilience
queuectl enqueue panic_demo '{"danger_level": "critical"}'

# 5. Enqueue a delayed job scheduled for 5 minutes in the future
queuectl enqueue email '{"to": "charlie@example.com", "body": "Delayed message"}' --delay 5m

echo "Database seeded successfully!"
