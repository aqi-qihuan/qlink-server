"""跳转读路由验证: 直接访问存量短链 x3, 不依赖新建(绕开流量包额度)"""
import time, urllib.request, urllib.error

GW = "http://127.0.0.1:8888"
CODES = ["a3z3JYk0", "021Tgota"]  # 在库存量短链: a3z3JYk0→db_a.short_link_0, 021Tgota→db_0.short_link_a

class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, *a, **k):
        return None

op = urllib.request.build_opener(NoRedirect)
ok = total = 0
for code in CODES:
    for i in range(3):
        total += 1
        try:
            with op.open(f"{GW}/{code}", timeout=8) as r:
                st = r.status
        except urllib.error.HTTPError as e:
            st = e.code
        except Exception as e:
            st = str(e)
        print(f"visit {code} #{i+1}: HTTP {st}")
        if st in (200, 301, 302, 307, 308):
            ok += 1
        time.sleep(0.5)
print(f"VISITS_OK={ok}/{total}")
