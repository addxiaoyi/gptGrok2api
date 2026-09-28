#!/bin/bash
# 简化版 iCloud 登录脚本（自动处理 2FA）
# 用法：./icloud-login-simple.sh <apple_id> <password> [2fa_code]

BASE="${ICLOUD_API_BASE:-http://127.0.0.1:8080}"
AUTH="Authorization: Bearer ${ADMIN_KEY:-admin-secret-key}"

APPLE_ID=$1
PASSWORD=$2
FORCE_2FA_CODE=${3:-}

if [[ -z "$APPLE_ID" || -z "$PASSWORD" ]]; then
    echo "用法：$0 <apple_id> <password> [2fa_code]"
    exit 1
fi

# 生成设备指纹
FINGERPRINT=$(head -c 8 /dev/urandom | xxd -p)

echo "🔐 发起登录 (指纹: $FINGERPRINT)..."
RESP=$(curl -s -X POST \
    -H "$AUTH" -H "Content-Type: application/json" \
    -d "{\"apple_id\":\"$APPLE_ID\",\"password\":\"$PASSWORD\",\"two_factor_method\":\"app\",\"device_fingerprint\":\"$FINGERPRINT\"}" \
    "$BASE/api/icloud/icloud/protocol-login/start")

NEEDS_2FA=$(echo "$RESP" | jq -r '.needs_2fa // false')
PENDING_ID=$(echo "$RESP" | jq -r '.pending_id // ""')

if [[ "$NEEDS_2FA" == "true" ]]; then
    echo "⚠️  需要 2FA: $PENDING_ID"
    
    CODE=${FORCE_2FA_CODE:-}
    if [[ -z "$CODE" ]]; then
        read -p "输入 6 位验证码：" CODE
    fi
    
    if [[ ${#CODE} -ne 6 ]]; then
        echo "错误：验证码必须是 6 位"
        exit 1
    fi
    
    echo "📱 提交验证码..."
    RESP=$(curl -s -X POST \
        -H "$AUTH" -H "Content-Type: application/json" \
        -d "{\"pending_id\":\"$PENDING_ID\",\"code\":\"$CODE\",\"method\":\"trusted_device\"}" \
        "$BASE/api/icloud/icloud/protocol-login/2fa")
fi

SUCCESS=$(echo "$RESP" | jq -r '.success // false')
if [[ "$SUCCESS" == "true" ]]; then
    echo "✅ 登录成功!"
    echo "$RESP" | jq '.session' 2>/dev/null || true
else
    echo "❌ 失败: $(echo "$RESP" | jq -r '.message')"
    exit 1
fi
