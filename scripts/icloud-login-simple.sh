#!/bin/bash
# 简化版 iCloud 登录脚本（安全输入版）
# 用法：
#   ./icloud-login-simple.sh <apple_id>
#   密码从 ICLOUD_PASSWORD 环境变量读取，或交互式输入
#   2FA 验证码自动隐藏输入

set -euo pipefail

BASE="${ICLOUD_API_BASE:-http://127.0.0.1:8080}"
AUTH="Authorization: Bearer ${ADMIN_KEY:-admin-secret-key}"

APPLE_ID=$1

if [[ -z "$APPLE_ID" ]]; then
    echo "用法：$0 <apple_id>"
    echo "密码通过 ICLOUD_PASSWORD 环境变量提供，或脚本会提示交互式输入"
    exit 1
fi

# 安全获取密码：环境变量 > 交互式隐藏输入
if [[ -n "${ICLOUD_PASSWORD:-}" ]]; then
    PASSWORD="$ICLOUD_PASSWORD"
else
    read -rsp "请输入 Apple ID 密码：" PASSWORD
    echo ""
fi

if [[ -z "$PASSWORD" ]]; then
    echo "错误：密码不能为空"
    exit 1
fi

# 生成设备指纹
FINGERPRINT=$(head -c 8 /dev/urandom | xxd -p)

echo "🔐 发起登录 (指纹: $FINGERPRINT)..."
RESP=$(curl -s -X POST \
    -H "$AUTH" -H "Content-Type: application/json" \
    -d "{\"apple_id\":\"$APPLE_ID\",\"password\":\"$PASSWORD\",\"two_factor_method\":\"app\",\"device_fingerprint\":\"$FINGERPRINT\"}" \
    "$BASE/api/icloud/icloud/protocol-login/start")

# 清理内存中的密码
PASSWORD=""

NEEDS_2FA=$(echo "$RESP" | jq -r '.needs_2fa // false')
PENDING_ID=$(echo "$RESP" | jq -r '.pending_id // ""')

if [[ "$NEEDS_2FA" == "true" ]]; then
    echo "⚠️  需要 2FA: $PENDING_ID"
    
    CODE=""
    read -rsp "请输入 6 位验证码：" CODE
    echo ""
    
    if [[ ${#CODE} -ne 6 ]]; then
        echo "错误：验证码必须是 6 位"
        exit 1
    fi
    
    echo "📱 提交验证码..."
    RESP=$(curl -s -X POST \
        -H "$AUTH" -H "Content-Type: application/json" \
        -d "{\"pending_id\":\"$PENDING_ID\",\"code\":\"$CODE\",\"method\":\"trusted_device\"}" \
        "$BASE/api/icloud/icloud/protocol-login/2fa")
    
    # 清理内存中的验证码
    CODE=""
fi

SUCCESS=$(echo "$RESP" | jq -r '.success // false')
if [[ "$SUCCESS" == "true" ]]; then
    echo "✅ 登录成功!"
    echo "$RESP" | jq '.session' 2>/dev/null || true
else
    echo "❌ 失败：$(echo "$RESP" | jq -r '.message')"
    exit 1
fi
