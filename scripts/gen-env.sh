#!/bin/sh
# Generate random RustFS credentials for development.
# Usage: ./scripts/gen-env.sh >> .env
echo "RUSTFS_ACCESS_KEY_ID=acb-$(openssl rand -hex 10)"
echo "RUSTFS_SECRET_ACCESS_KEY=$(openssl rand -hex 20)"
