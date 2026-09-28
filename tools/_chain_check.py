"""全链路验证: 登录 -> 建组 -> 建短链 -> 访问跳转 x3 (产生 PV)"""
import json, time, sys, urllib.request, urllib.error

GW = "http://127.0.0.1:8888"
opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))

def call(method, path, data=None, token=None):
    req = urllib.request.Request(GW + path, method=method)
    req.add_header("Content-Type", "application/json")
    if token:
        req.add_header("token", token)  # 服务端 auth 用 token header, 非 Bearer
    body = json.dumps(data).encode() if data is not None else None
    try:
        with opener.open(req, body, timeout=8) as r:
            return r.status, json.loads(r.read().decode() or "{}")
    except urllib.error.HTTPError as e:
        try:
            return e.code, json.loads(e.read().decode() or "{}")
        except Exception:
            return e.code, {}

# 1. 登录
st, r = call("POST", "/account-server/api/account/v1/login", {"phone": "13900139003", "pwd": "Test123456!"})
d = r.get("data")
token = d if isinstance(d, str) else (d or {}).get("token")
print(f"login: HTTP {st}, token={'OK' if token else 'MISSING: ' + str(r)[:120]}")
if not token:
    sys.exit(1)

# 2. 建组
st, r = call("POST", "/link-server/api/group/v1/add", {"title": "链路验证组-" + str(int(time.time()))}, token=token)
d = r.get("data")
gid = d.get("id") if isinstance(d, dict) else d
print(f"group: HTTP {st}, gid={gid}")

# 3. 建短链
st, r = call("POST", "/link-server/api/link/v1/add",
             {"title": "链路验证", "originalUrl": "https://github.com/aqi", "groupId": gid}, token=token)
d = r.get("data") or {}
code = d.get("code") or d.get("shortCode")
print(f"create link: HTTP {st}, code={code}")
if not code:
    print("raw:", str(r)[:200]); sys.exit(1)

# 4. 访问跳转 x3 (不跟随重定向, 301/302 即为成功)
class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, *a, **k):
        return None
op2 = urllib.request.build_opener(NoRedirect)
pv_ok = 0
for i in range(3):
    try:
        with op2.open(f"{GW}/{code}", timeout=8) as resp:
            st2 = resp.status
    except urllib.error.HTTPError as e:
        st2 = e.code
    except Exception as e:
        st2 = str(e)
    print(f"visit#{i+1}: HTTP {st2}")
    if st2 in (200, 301, 302, 307, 308):
        pv_ok += 1
    time.sleep(0.5)
print(f"VISITS_OK={pv_ok}/3")
