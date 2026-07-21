#!/usr/bin/env python3
"""qlink-server 全量联调测试脚本 v2（修复字段名）
覆盖：M01→M02→M03→M04→M05→M07→M09→M10→M11→M12→M14
"""
import urllib.request, urllib.error, json, http.client, redis, sys, os

GATEWAY = os.environ.get("GATEWAY", "http://localhost:8888")
REDIS_HOST = os.environ.get("REDIS_HOST", "localhost")
REDIS_PWD = os.environ.get("REDIS_PWD", "")
UA = "qlink-test-client"
PHONE = "13900139002"
PWD = "Test123456!"

pass_c = fail_c = skip_c = 0
V = {}  # 变量存储

def api(method, path, data=None, token=None):
    url = f"{GATEWAY}{path}"
    body = json.dumps(data).encode() if data else None
    h = {"Content-Type": "application/json", "User-Agent": UA}
    if token: h["token"] = token
    req = urllib.request.Request(url, data=body, headers=h, method=method)
    try:
        resp = urllib.request.urlopen(req, timeout=15)
        return json.loads(resp.read().decode())
    except urllib.error.HTTPError as e:
        try: return json.loads(e.read().decode())
        except: return {"code": -999, "msg": f"HTTP {e.code}"}
    except Exception as e:
        return {"code": -999, "msg": str(e)}

def test(name, r, expect=0):
    global pass_c, fail_c
    code = r.get("code", -999)
    ok = code == expect
    pass_c += ok; fail_c += not ok
    st = "PASS" if ok else "FAIL"
    msg = str(r.get("msg", ""))[:50]
    d = json.dumps(r.get("data"), ensure_ascii=False)[:70] if r.get("data") else ""
    print(f"  [{st}] {name}: code={code} {msg} {d}")
    return r

def skip(name, reason=""):
    global skip_c; skip_c += 1
    print(f"  [SKIP] {name}: {reason}")

rdb = redis.Redis(host=REDIS_HOST, port=6379, password=REDIS_PWD, decode_responses=True)

# ====== M01: 基础链路 ======
print("\n===== M01: 基础链路 =====")
for svc, path in [("account","/account-server/health"),("link","/link-server/health"),("shop","/shop-server/health"),("ai","/ai-server/health"),("gateway","/health")]:
    r = api("GET", path)
    test(f"健康-{svc}", r if "code" in r else {"code":0})

# 验证码
conn = http.client.HTTPConnection("localhost", 8001, timeout=10)
conn.request("GET", "/api/notify/v1/captcha", headers={"User-Agent": UA})
conn.getresponse().read()
ck = rdb.keys("account-service:captcha:*")
captcha = rdb.get(ck[-1]) if ck else ""
print(f"  [INFO] captcha={captcha}")

# 发送短信码
api("POST", "/account-server/api/account/v1/send_code", {"captcha": captcha, "to": PHONE})
cv = rdb.get(f"code:USER_REGISTER:{PHONE}") or ""
sms = cv.split("_")[0]
print(f"  [INFO] sms={sms}")

# 注册 + 登录
test("注册", api("POST", "/account-server/api/account/v1/register", {"phone": PHONE, "pwd": PWD, "code": sms, "username": "tester2"}))
r = api("POST", "/account-server/api/account/v1/login", {"phone": PHONE, "pwd": PWD})
test("登录", r)
V["token"] = r.get("data","") if isinstance(r.get("data"), str) else ""
if not V["token"]: print("  [FATAL] 无 token"); sys.exit(1)
print(f"  [INFO] token={V['token'][:35]}...")

test("用户信息", api("GET", "/account-server/api/account/v1/detail", token=V["token"]))
r = api("GET", "/account-server/api/traffic/v1/claim_free", token=V["token"])
test("领取流量包", r)  # 可能已领取过
test("流量包列表", api("GET", "/account-server/api/traffic/v1/page?page=1&size=10", token=V["token"]))

# ====== M02: 分组 ======
print("\n===== M02: 分组管理 =====")
gids = []
for gn in ["技术博客", "产品链接", "营销活动"]:
    r = api("POST", "/link-server/api/group/v1/add", {"title": gn}, token=V["token"])
    test(f"新增分组-{gn}", r)
    if isinstance(r.get("data"), dict): gids.append(r["data"].get("id",0))
V["gid"] = gids[0] if gids else 0
print(f"  [INFO] groupId={V['gid']}")
test("分组列表", api("GET", "/link-server/api/group/v1/list", token=V["token"]))
test("组详情", api("GET", f"/link-server/api/group/v1/detail/{V['gid']}", token=V["token"]))
test("更新组", api("PUT", "/link-server/api/group/v1/update", {"id": V["gid"], "title": "技术博客-改"}, token=V["token"]))

# ====== M03: 域名 ======
print("\n===== M03: 域名管理 =====")
test("域名列表", api("GET", "/link-server/api/domain/v1/list", token=V["token"]))

# ====== M04: 短链核心 ======
print("\n===== M04: 短链核心 =====")
r = api("POST", "/link-server/api/link/v1/add", {"title":"GitHub","originalUrl":"https://github.com","groupId": V["gid"]}, token=V["token"])
test("创建短链", r)
V["code"] = r.get("data",{}).get("code","") if isinstance(r.get("data"), dict) else ""
V["mid"] = r.get("data",{}).get("id",0) if isinstance(r.get("data"), dict) else 0
print(f"  [INFO] code={V['code']} mappingId={V['mid']}")

# 分页
r = api("POST", "/link-server/api/link/v1/page", {"page":1,"size":10,"groupId": V["gid"]}, token=V["token"])
test("分页查找", r)
if isinstance(r.get("data"), dict) and r["data"].get("list"):
    V["mid"] = r["data"]["list"][0].get("id", V["mid"])

# 详情（需要 groupId + mappingId）
test("短链详情", api("POST", "/link-server/api/link/v1/detail", {"groupId": V["gid"], "mappingId": V["mid"]}, token=V["token"]))

# 更新
test("更新短链", api("POST", "/link-server/api/link/v1/update", {"code": V["code"], "title": "GitHub-改", "groupId": V["gid"]}, token=V["token"]))

# 状态切换（需要 code + groupId + state="ACTIVE"/"INACTIVE"）
test("短链-禁用", api("POST", "/link-server/api/link/v1/status", {"code": V["code"], "groupId": V["gid"], "state": "INACTIVE"}, token=V["token"]))
test("短链-启用", api("POST", "/link-server/api/link/v1/status", {"code": V["code"], "groupId": V["gid"], "state": "ACTIVE"}, token=V["token"]))

# 统计
test("短链统计", api("POST", "/link-server/api/link/v1/summary", {"code": V["code"]}, token=V["token"]))

# URL安全检查
test("安全检查-正常", api("POST", "/link-server/api/link/v1/url_safe_check", {"url": "https://github.com"}, token=V["token"]))
test("安全检查-可疑", api("POST", "/link-server/api/link/v1/url_safe_check", {"url": "http://phishing-test.example.com/fake"}, token=V["token"]))

# ====== M05: AB测试 ======
print("\n===== M05: AB测试 =====")
r = api("POST", "/link-server/api/ab_test/v1/add", {
    "shortLinkCode": V["code"],
    "groupId": V["gid"],
    "name": "AB测试-按钮颜色",
    "description": "测试不同按钮颜色",
    "trafficSplit": "50",
    "variants": [
        {"name": "控制组", "targetUrl": "https://github.com", "weight": 50, "isControl": 1},
        {"name": "实验组", "targetUrl": "https://gitlab.com", "weight": 50, "isControl": 0},
    ]
}, token=V["token"])
test("创建AB测试", r)
V["abId"] = r.get("data",{}).get("id",0) if isinstance(r.get("data"), dict) else 0

test("AB列表", api("POST", "/link-server/api/ab_test/v1/page", {"page":1,"size":10}, token=V["token"]))

# 启动+停止+删除
r = api("POST", "/link-server/api/ab_test/v1/start", {"id": V["abId"]}, token=V["token"])
test("启动AB测试", r)
r = api("POST", "/link-server/api/ab_test/v1/stop", {"id": V["abId"]}, token=V["token"])
test("停止AB测试", r)
r = api("POST", "/link-server/api/ab_test/v1/del", {"id": V["abId"]}, token=V["token"])
test("删除AB测试", r)

# ====== M07: 品牌 ======
print("\n===== M07: 品牌定制 =====")
test("品牌保存", api("POST", "/link-server/api/brand_config/v1/save", {"siteName":"滴云短链","logoUrl":"https://example.com/logo.png","primaryColor":"#1890ff"}, token=V["token"]))
test("品牌查看", api("GET", "/link-server/api/brand_config/v1/get", token=V["token"]))

# ====== M09: 审计 ======
print("\n===== M09: 审计日志 =====")
test("操作日志", api("POST", "/link-server/api/operation_log/v1/page", {"page":1,"size":10}, token=V["token"]))
test("操作类型", api("GET", "/link-server/api/operation_log/v1/action_types", token=V["token"]))

# ====== M10: API Token ======
print("\n===== M10: API Token =====")
test("创建Token", api("POST", "/account-server/api/account/v1/api_token/create", {"name":"test-token"}, token=V["token"]))
test("Token列表", api("POST", "/account-server/api/account/v1/api_token/list", {"page":1,"size":10}, token=V["token"]))

# ====== M11: 商品支付 ======
print("\n===== M11: 商品与支付 =====")
r = api("GET", "/shop-server/api/product/v1/list", token=V["token"])
test("商品列表", r)
# 获取商品价格
product_price = 0.01
if isinstance(r.get("data"), list) and r["data"]:
    for p in r["data"]:
        if p.get("id") == 2:
            product_price = p.get("amount", 0.01) or p.get("price", 0.01)
            break
print(f"  [INFO] product price={product_price}")

test("商品详情", api("GET", "/shop-server/api/product/v1/detail/2", token=V["token"]))
r = api("GET", "/shop-server/api/order/v1/get_token", token=V["token"])
test("下单令牌", r)
order_token = r.get("data","") if isinstance(r.get("data"), str) else ""

# 下单-微信（需要 token + totalAmount + realPayAmount + payType=WECHAT_PAY）
r = api("POST", "/shop-server/api/order/v1/confirm", {
    "productId": 2, "buyNum": 1, "payType": "WECHAT_PAY",
    "totalAmount": product_price, "realPayAmount": product_price,
    "token": order_token, "clientType": "PC",
}, token=V["token"])
test("下单-微信", r)
if isinstance(r.get("data"), dict):
    V["orderNo"] = r["data"].get("outTradeNo","") or r["data"].get("orderNo","")

test("订单分页", api("POST", "/shop-server/api/order/v1/page", {"page":1,"size":10}, token=V["token"]))
if V.get("orderNo"):
    test("订单状态", api("GET", f"/shop-server/api/order/v1/query_state?out_trade_no={V['orderNo']}", token=V["token"]))

# 重新获取下单令牌（防止重复提交）
r = api("GET", "/shop-server/api/order/v1/get_token", token=V["token"])
order_token2 = r.get("data","") if isinstance(r.get("data"), str) else ""
r = api("POST", "/shop-server/api/order/v1/confirm", {
    "productId": 2, "buyNum": 1, "payType": "ALI_PAY",
    "totalAmount": product_price, "realPayAmount": product_price,
    "token": order_token2, "clientType": "PC",
}, token=V["token"])
test("下单-支付宝", r)

# ====== M12: AI ======
print("\n===== M12: AI功能 =====")
r = api("POST", "/ai-server/api/ai/v1/check_safety", {"url": "https://github.com"}, token=V["token"])
test("URL安全检测AI", r)

# ====== M08: 数据统计 ======
print("\n===== M08: 数据统计 =====")
try:
    urllib.request.urlopen("http://localhost:8002/health", timeout=3)
    data_ok = True
except:
    data_ok = False
if not data_ok:
    skip("数据统计全部", "data 服务未运行（ClickHouse 不可达）")
else:
    test("仪表盘", api("POST", "/data-server/api/visit_stats/v1/dashboard", {"startDate":"2026-07-01","endDate":"2026-07-19"}, token=V["token"]))

# ====== M14: 清理 ======
print("\n===== M14: 清理 =====")
# 批量删除需要 groupId + ids
test("批量删除短链", api("POST", "/link-server/api/link/v1/batch_delete", {"groupId": V["gid"], "ids": [V["mid"]]}, token=V["token"]))

# ====== 汇总 ======
print(f"\n{'='*50}")
print(f"联调结果: PASS={pass_c}  FAIL={fail_c}  SKIP={skip_c}")
print(f"{'='*50}")
print("✅ 全部通过！" if fail_c == 0 else "⚠️ 有失败用例，检查上方 FAIL 项")
