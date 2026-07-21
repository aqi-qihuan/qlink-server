#!/usr/bin/env python3
"""qlink-server API 联调测试脚本
自动完成：健康检查 → 获取验证码 → 发送短信码 → 注册 → 登录 → 创建短链
"""
import urllib.request
import urllib.error
import json
import hashlib
import time
import redis
import os
from dotenv import load_dotenv

# 自动加载 .env（gitignored，含真实凭据）
load_dotenv(os.path.join(os.path.dirname(__file__), "..", ".env"))

# ---- 配置（从 .env / 环境变量读取，不硬编码敏感信息）----
ACCOUNT_URL = os.environ.get("ACCOUNT_URL", "http://localhost:8001")
LINK_URL    = os.environ.get("LINK_URL", "http://localhost:8003")
GATEWAY_URL = os.environ.get("GATEWAY_URL", "http://localhost:8888")
REDIS_HOST  = os.environ.get("REDIS_HOST", "localhost")
REDIS_PORT  = int(os.environ.get("REDIS_PORT", "6379"))
REDIS_PWD   = os.environ.get("REDIS_PWD", "")
USER_AGENT  = "qlink-test-client"
TEST_PHONE  = "13800138001"
TEST_PWD    = "Test123456!"

# ---- 工具函数 ----
def http_post(url, data, headers=None):
    body = json.dumps(data).encode("utf-8")
    h = {"Content-Type": "application/json", "User-Agent": USER_AGENT}
    if headers:
        h.update(headers)
    req = urllib.request.Request(url, data=body, headers=h, method="POST")
    try:
        resp = urllib.request.urlopen(req, timeout=10)
        return json.loads(resp.read().decode())
    except urllib.error.HTTPError as e:
        return json.loads(e.read().decode())
    except Exception as e:
        return {"error": str(e)}

def http_get(url, headers=None):
    h = {"User-Agent": USER_AGENT}
    if headers:
        h.update(headers)
    req = urllib.request.Request(url, headers=h, method="GET")
    try:
        resp = urllib.request.urlopen(req, timeout=10)
        return resp
    except Exception as e:
        return None

def md5(s):
    return hashlib.md5(s.encode()).hexdigest()

def print_result(step, result):
    status = result.get("code", result.get("status", "unknown"))
    print(f"  [{step}] code={status} data={json.dumps(result, ensure_ascii=False)[:200]}")

# ---- Redis 连接 ----
rdb = redis.Redis(host=REDIS_HOST, port=REDIS_PORT, password=REDIS_PWD, decode_responses=True)
print(f"Redis connected: {rdb.ping()}")

# ---- Step 1: 健康检查 ----
print("\n=== Step 1: Health Checks ===")
for name, url in [("account", ACCOUNT_URL), ("link", LINK_URL), ("gateway", GATEWAY_URL)]:
    resp = http_get(f"{url}/health")
    if resp:
        body = resp.read().decode()[:80]
        print(f"  {name}: OK - {body}")
    else:
        print(f"  {name}: FAIL")

# ---- Step 2: 获取验证码图片（同时存入 Redis）----
print("\n=== Step 2: Get Captcha ===")
resp = http_get(f"{ACCOUNT_URL}/api/notify/v1/captcha")
if resp:
    print(f"  captcha image: {resp.status} ({resp.headers.get('Content-Type')})")
else:
    print("  captcha: FAIL")
    exit(1)

# 从 Redis 读取验证码文本（搜索所有 captcha 键，取最新的）
captcha_keys = rdb.keys("account-service:captcha:*")
if not captcha_keys:
    print("  ERROR: captcha text not found in Redis")
    exit(1)
captcha_key = captcha_keys[-1]  # 取最后一个
captcha_text = rdb.get(captcha_key)
print(f"  captcha key: {captcha_key}")
print(f"  captcha text: {captcha_text}")

# ---- Step 3: 发送短信验证码 ----
print("\n=== Step 3: Send SMS Code ===")
result = http_post(f"{ACCOUNT_URL}/api/notify/v1/send_code", {
    "captcha": captcha_text,
    "to": TEST_PHONE
})
print_result("send_code", result)

# 从 Redis 读取短信验证码（搜索匹配的 code 键）
code_keys = rdb.keys(f"code:*:{TEST_PHONE}")
if not code_keys:
    # 尝试搜索所有 code 键
    code_keys = rdb.keys("code:*")
if not code_keys:
    print("  ERROR: verification code not found in Redis")
    exit(1)
code_key = code_keys[-1]
code_value = rdb.get(code_key)
print(f"  code key: {code_key}")
print(f"  code value: {code_value}")

if not code_value:
    print("  ERROR: verification code not found in Redis")
    exit(1)

# code_value format: "code_timestamp"
sms_code = code_value.split("_")[0]
print(f"  sms code: {sms_code}")

# ---- Step 4: 注册 ----
print("\n=== Step 4: Register ===")
result = http_post(f"{ACCOUNT_URL}/api/account/v1/register", {
    "phone": TEST_PHONE,
    "pwd": TEST_PWD,
    "code": sms_code,
    "username": "qlink_tester"
})
print_result("register", result)

# 如果注册失败（手机号已存在），继续登录
# ---- Step 5: 登录 ----
print("\n=== Step 5: Login ===")
result = http_post(f"{ACCOUNT_URL}/api/account/v1/login", {
    "phone": TEST_PHONE,
    "pwd": TEST_PWD
})
print_result("login", result)

token = None
if "data" in result and isinstance(result["data"], str):
    token = result["data"]
elif "data" in result and isinstance(result["data"], dict):
    token = result["data"].get("token")
if not token:
    print("  ERROR: no token in login response")
    print(f"  full response: {json.dumps(result, ensure_ascii=False)}")
    exit(1)

print(f"  token: {token[:60]}...")

# ---- Step 6: 创建短链 ----
print("\n=== Step 6: Create Short Link ===")
result = http_post(f"{LINK_URL}/api/link/v1/add", {
    "title": "qlink-test-link",
    "originalUrl": "https://github.com",
    "groupId": 0,
    "domainId": 0,
    "domainType": "",
    "expired": "",
}, headers={"token": token})
print_result("create_link", result)

print("\n=== 联调测试完成 ===")
