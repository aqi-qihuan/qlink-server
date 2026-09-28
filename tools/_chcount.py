"""ClickHouse 各表行数统计(读 .env 凭据, 勿硬编码)"""
import os
import urllib.parse
import urllib.request

import dotenv

dotenv.load_dotenv(os.path.join(os.path.dirname(__file__), "..", ".env"))
host = os.environ.get("CLICKHOUSE_ADDR", "localhost:8123").split(":")[0]
user = os.environ.get("CLICKHOUSE_USER", "default")
pwd = os.environ.get("CLICKHOUSE_PWD", "")
db = os.environ.get("CLICKHOUSE_DB", "visit_stats")
auth = base64_auth = __import__("base64").b64encode(f"{user}:{pwd}".encode()).decode()
H = {"Authorization": f"Basic {auth}"}


def q(sql):
    url = f"http://{host}:8123/?query=" + urllib.parse.quote(sql)
    req = urllib.request.Request(url, headers=H)
    with urllib.request.urlopen(req, timeout=8) as r:
        return r.read().decode().strip()


tables = [t for t in q(f"SHOW TABLES FROM {db}").splitlines() if t.strip()]
total = 0
for t in tables:
    try:
        n = int(q(f"SELECT count() FROM {db}.`{t}`"))
        print(f"{t}: {n}")
        total += n
    except Exception as e:
        print(f"{t}: ERR {str(e)[:60]}")
print(f"CH_TOTAL_ROWS={total}")
