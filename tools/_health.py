import time, urllib.request, sys
opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
checks = {"gateway":8888, "account":8001, "data":8002, "link":8003, "shop":8005, "ai":8006}
ok = 0
for name, p in checks.items():
    for a in range(5):
        try:
            with opener.open(f"http://127.0.0.1:{p}/health", timeout=3) as r:
                print(f"{name:8s} :{p}  HTTP {r.status}")
                ok += (r.status == 200)
                break
        except Exception:
            if a == 4:
                print(f"{name:8s} :{p}  FAIL")
            else:
                time.sleep(2)
sys.exit(0 if ok == 6 else 1)
