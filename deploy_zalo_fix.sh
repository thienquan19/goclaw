#!/bin/bash
# ============================================================
# Deploy GoClaw fork (fix Zalo group pairing spam) lên VPS
# Chạy script này trên VPS: ssh root@34.171.30.28
# ============================================================

set -euo pipefail

VPS_IP="34.171.30.28"
REPO_URL="https://github.com/thienquan19/goclaw.git"
BRANCH="dev"
DEPLOY_DIR="/opt/goclaw-build"

echo "╔════════════════════════════════════════════╗"
echo "║  Deploy GoClaw Fork - Fix Zalo Group Spam  ║"
echo "╚════════════════════════════════════════════╝"
echo ""

# 1. Clone/pull fork repo
if [ -d "$DEPLOY_DIR" ]; then
  echo "📂 Updating existing repo..."
  cd "$DEPLOY_DIR"
  git fetch origin
  git checkout "$BRANCH"
  git pull origin "$BRANCH"
else
  echo "📥 Cloning fork repo..."
  git clone -b "$BRANCH" "$REPO_URL" "$DEPLOY_DIR"
  cd "$DEPLOY_DIR"
fi
echo "✅ Source code ready"

# 2. Build Docker image from fork
echo ""
echo "🔨 Building Docker image (this may take 5-10 minutes)..."
docker build \
  --build-arg VERSION=dev-fix-zalo \
  --build-arg ENABLE_EMBEDUI=true \
  --build-arg ENABLE_PYTHON=true \
  -t goclaw-fork:latest \
  -f Dockerfile .
echo "✅ Docker image built: goclaw-fork:latest"

# 3. Find running goclaw container
echo ""
echo "🔍 Finding running GoClaw container..."
CONTAINER_NAME=$(docker ps --format '{{.Names}}' | grep -i goclaw | head -1)

if [ -z "$CONTAINER_NAME" ]; then
  echo "⚠️  No running GoClaw container found!"
  echo "   Looking for docker-compose setup..."
  
  # Try to find docker-compose file
  COMPOSE_DIR=$(find /opt -name "docker-compose*.yml" -path "*goclaw*" -exec dirname {} \; 2>/dev/null | head -1)
  if [ -z "$COMPOSE_DIR" ]; then
    COMPOSE_DIR="/opt/goclaw"
  fi
  
  echo "   Using compose dir: $COMPOSE_DIR"
else
  echo "   Found container: $CONTAINER_NAME"
  COMPOSE_DIR=$(docker inspect "$CONTAINER_NAME" --format '{{index .Config.Labels "com.docker.compose.project.working_dir"}}' 2>/dev/null || echo "/opt/goclaw")
fi

# 4. Update docker-compose to use fork image
echo ""
echo "📝 Updating docker-compose to use fork image..."

# Check if docker-compose.override.yml exists, create/update it
cat > "$COMPOSE_DIR/docker-compose.override.yml" << 'EOF'
# Override to use fork image instead of upstream
services:
  goclaw:
    image: goclaw-fork:latest
    build:
      context: /opt/goclaw-build
      dockerfile: Dockerfile
      args:
        ENABLE_EMBEDUI: "true"
        ENABLE_PYTHON: "true"
        VERSION: "dev-fix-zalo"
EOF
echo "✅ Override file created"

# 5. Restart with new image
echo ""
echo "🔄 Restarting GoClaw with fork image..."
cd "$COMPOSE_DIR"

# Use prepare-compose.sh if available
if [ -f "prepare-compose.sh" ]; then
  bash prepare-compose.sh
fi

docker compose up -d --force-recreate
echo "✅ GoClaw restarted with fix!"

# 6. Wait and check logs
echo ""
echo "⏳ Waiting 10 seconds for startup..."
sleep 10

echo "📋 Recent logs:"
docker compose logs --tail=20 goclaw 2>/dev/null || docker logs --tail=20 "$CONTAINER_NAME" 2>/dev/null

echo ""
echo "╔════════════════════════════════════════════╗"
echo "║           🎉 DEPLOY HOÀN TẤT!             ║"
echo "║                                            ║"
echo "║  Fix đã apply:                             ║"
echo "║  ✅ Group pairing → DM thay vì spam nhóm   ║"
echo "║  ✅ Debounce tăng từ 60s → 24h cho nhóm    ║"
echo "║  ✅ Reply bằng tiếng Việt                   ║"
echo "║  ✅ Vẫn hiện trên Web Dashboard → Approve   ║"
echo "║                                            ║"
echo "║  Web Dashboard: http://$VPS_IP:18790       ║"
echo "╚════════════════════════════════════════════╝"
