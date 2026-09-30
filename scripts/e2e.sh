#!/bin/sh
# End-to-end API smoke test for BunkrDownloader (gin branch).
# Usage: sh e2e.sh [base_url]
set -e
BASE="${1:-http://127.0.0.1:8765}"
PASS=0; FAIL=0
TMP=$(mktemp -d)

say()  { printf '\n\033[1;36m== %s\033[0m\n' "$*"; }
ok()   { PASS=$((PASS+1)); printf '  \033[0;32mPASS\033[0m %s\n' "$*"; }
bad()  { FAIL=$((FAIL+1)); printf '  \033[0;31mFAIL\033[0m %s\n' "$*"; }
chk()  { # chk <desc> <actual> <expected>
  if [ "$2" = "$3" ]; then ok "$1 ($2)"; else bad "$1: got '$2' want '$3'"; fi
}

# jsonfield <json> <python-expr on d>
jf() { python -c "import sys,json;d=json.load(sys.stdin);print($1)" 2>/dev/null || echo "<none>"; }

say "1. health"
H=$(curl -sS "$BASE/api/health")
chk "service up"          "$(echo "$H" | jf "d['service']")" "bunkr-web"
chk "aria2 available"     "$(echo "$H" | jf "d['aria2']['available']")" "True"
printf '  aria2 version: %s\n' "$(echo "$H" | jf "d['aria2']['version']")"

say "2. register"
U="smoke$RANDOM"
R=$(curl -sS -X POST "$BASE/api/auth/register" -H 'Content-Type: application/json' \
     -d "{\"username\":\"$U\",\"email\":\"$U@test.local\",\"password\":\"passw0rd\"}")
TOKEN=$(echo "$R" | jf "d['token']")
[ -n "$TOKEN" ] && [ "$TOKEN" != "<none>" ] && ok "token issued" || { bad "no token: $R"; exit 1; }
AUTH="Authorization: Bearer $TOKEN"
chk "plan free"      "$(echo "$R" | jf "d['user']['plan']")" "free"
chk "links limit 5"  "$(echo "$R" | jf "d['quota']['links_limit']")" "5"
chk "files limit 50" "$(echo "$R" | jf "d['quota']['files_limit']")" "50"
chk "free not member" "$(echo "$R" | jf "d['quota']['is_member']")" "False"

say "3. duplicate + weak-password rejection"
D=$(curl -sS -o "$TMP/d" -w "%{http_code}" -X POST "$BASE/api/auth/register" -H 'Content-Type: application/json' \
     -d "{\"username\":\"$U\",\"email\":\"other@test.local\",\"password\":\"passw0rd\"}")
chk "duplicate username -> 400" "$D" "400"
chk "  code" "$(cat "$TMP/d" | jf "d['error']['code']")" "username_taken"
D=$(curl -sS -o "$TMP/d" -w "%{http_code}" -X POST "$BASE/api/auth/register" -H 'Content-Type: application/json' \
     -d '{"username":"weakpwd","email":"w@test.local","password":"12"}')
chk "weak password -> 400" "$D" "400"
chk "  code" "$(cat "$TMP/d" | jf "d['error']['code']")" "weak_password"

say "4. login"
L=$(curl -sS -X POST "$BASE/api/auth/login" -H 'Content-Type: application/json' \
     -d "{\"account\":\"$U@test.local\",\"password\":\"passw0rd\"}")
T2=$(echo "$L" | jf "d['token']")
[ -n "$T2" ] && [ "$T2" != "<none>" ] && ok "login by email" || bad "login failed: $L"
L2=$(curl -sS -o /dev/null -w "%{http_code}" -X POST "$BASE/api/auth/login" -H 'Content-Type: application/json' \
     -d "{\"account\":\"$U\",\"password\":\"passw0rd\"}")
chk "login by username -> 200" "$L2" "200"
D=$(curl -sS -o "$TMP/d" -w "%{http_code}" -X POST "$BASE/api/auth/login" -H 'Content-Type: application/json' \
     -d "{\"account\":\"$U\",\"password\":\"wrongpass\"}")
chk "bad password -> 400" "$D" "400"

say "5. auth guard"
D=$(curl -sS -o "$TMP/d" -w "%{http_code}" "$BASE/api/tasks")
chk "no token -> 401" "$D" "401"
chk "  code" "$(cat "$TMP/d" | jf "d['error']['code']")" "unauthorized"
D=$(curl -sS -o "$TMP/d" -w "%{http_code}" "$BASE/api/tasks" -H "Authorization: Bearer garbage.token.here")
chk "bad token -> 401" "$D" "401"

say "6. URL validation"
D=$(curl -sS -o "$TMP/d" -w "%{http_code}" -X POST "$BASE/api/tasks" -H "$AUTH" \
     -H 'Content-Type: application/json' -d '{"url":"https://example.com/not-bunkr"}')
chk "non-bunkr url -> 400" "$D" "400"
chk "  code" "$(cat "$TMP/d" | jf "d['error']['code']")" "invalid_url"
D=$(curl -sS -o "$TMP/d" -w "%{http_code}" -X POST "$BASE/api/tasks" -H "$AUTH" \
     -H 'Content-Type: application/json' -d '{"url":"   "}')
chk "blank url -> 400" "$D" "400"

say "7. link quota (5 max)"
for i in 1 2 3 4 5; do
  C=$(curl -sS -o "$TMP/c" -w "%{http_code}" -X POST "$BASE/api/tasks" -H "$AUTH" \
       -H 'Content-Type: application/json' \
       -d "{\"url\":\"https://bunkr.si/a/SMOKE$i\",\"auto_start\":false}")
  chk "create link $i -> 201" "$C" "201"
done
Q=$(curl -sS "$BASE/api/tasks" -H "$AUTH" | jf "d['quota']['links_used']" 2>/dev/null)
D=$(curl -sS -o "$TMP/d" -w "%{http_code}" -X POST "$BASE/api/tasks" -H "$AUTH" \
     -H 'Content-Type: application/json' -d '{"url":"https://bunkr.si/a/SMOKE6","auto_start":false}')
chk "6th link -> 403" "$D" "403"
chk "  code" "$(cat "$TMP/d" | jf "d['error']['code']")" "quota_exceeded"
chk "  limit detail" "$(cat "$TMP/d" | jf "d['error']['details']['limit']")" "5"

say "8. batch quota is all-or-nothing"
D=$(curl -sS -o "$TMP/d" -w "%{http_code}" -X POST "$BASE/api/tasks" -H "$AUTH" \
     -H 'Content-Type: application/json' \
     -d '{"url":"https://bunkr.si/a/B1\nhttps://bunkr.si/a/B2","auto_start":false}')
chk "batch of 2 over quota -> 403" "$D" "403"

say "9. plans (public)"
P=$(curl -sS "$BASE/api/membership/plans")
chk "plan count" "$(echo "$P" | jf "len(d['plans'])")" "3"
chk "free limits" "$(echo "$P" | jf "d['plans'][0]['limits']['links']")" "5"
chk "monthly price" "$(echo "$P" | jf "d['plans'][1]['price_cents']")" "990"
chk "member unlimited" "$(echo "$P" | jf "d['plans'][1]['limits']['links']")" "-1"

say "10. membership purchase"
O=$(curl -sS -X POST "$BASE/api/membership/orders" -H "$AUTH" -H 'Content-Type: application/json' \
     -d '{"plan":"member_monthly"}')
OID=$(echo "$O" | jf "d['order']['id']")
chk "order created" "$(echo "$O" | jf "d['order']['status']")" "pending"
chk "order amount"   "$(echo "$O" | jf "d['order']['amount_cents']")" "990"
P2=$(curl -sS -X POST "$BASE/api/membership/orders/$OID/pay" -H "$AUTH" \
      -H 'Content-Type: application/json' -d '{"pay_method":"alipay"}')
chk "paid"          "$(echo "$P2" | jf "d['order']['status']")" "paid"
chk "plan upgraded" "$(echo "$P2" | jf "d['user']['plan']")" "member"
chk "links unlimited" "$(echo "$P2" | jf "d['quota']['links_unlimited']")" "True"
chk "files unlimited" "$(echo "$P2" | jf "d['quota']['files_unlimited']")" "True"
chk "concurrency 5"   "$(echo "$P2" | jf "d['quota']['concurrent_limit']")" "5"

say "11. quota lifted after upgrade"
C=$(curl -sS -o "$TMP/c" -w "%{http_code}" -X POST "$BASE/api/tasks" -H "$AUTH" \
     -H 'Content-Type: application/json' -d '{"url":"https://bunkr.si/a/AFTER1","auto_start":false}')
chk "member can exceed 5 links -> 201" "$C" "201"

say "12. redeem code"
U2="redeem$RANDOM"
T3=$(curl -sS -X POST "$BASE/api/auth/register" -H 'Content-Type: application/json' \
      -d "{\"username\":\"$U2\",\"email\":\"$U2@t.local\",\"password\":\"passw0rd\"}" | jf "d['token']")
RC=$(curl -sS -X POST "$BASE/api/membership/redeem" -H "Authorization: Bearer $T3" \
     -H 'Content-Type: application/json' -d '{"code":"BUNKR-MEMBER-2024"}')
chk "redeem -> member" "$(echo "$RC" | jf "d['user']['plan']")" "member"
D=$(curl -sS -o "$TMP/d" -w "%{http_code}" -X POST "$BASE/api/membership/redeem" \
     -H "Authorization: Bearer $T3" -H 'Content-Type: application/json' -d '{"code":"BUNKR-MEMBER-2024"}')
chk "reuse code -> 400" "$D" "400"
D=$(curl -sS -o "$TMP/d" -w "%{http_code}" -X POST "$BASE/api/membership/redeem" \
     -H "Authorization: Bearer $T3" -H 'Content-Type: application/json' -d '{"code":"NOPE"}')
chk "bad code -> 400" "$D" "400"

say "13. task CRUD + lifecycle"
T=$(curl -sS "$BASE/api/tasks?limit=5" -H "$AUTH")
chk "task list shape" "$(echo "$T" | jf "len(d['tasks'])>0")" "True"
TID=$(echo "$T" | jf "d['tasks'][0]['id']")
chk "task id numeric" "$(echo "$TID" | jf "str(int(d))")" "$TID"
G=$(curl -sS "$BASE/api/tasks/$TID" -H "$AUTH")
chk "task detail" "$(echo "$G" | jf "d['task']['id']")" "$TID"
F=$(curl -sS "$BASE/api/tasks/$TID/files" -H "$AUTH")
chk "files list shape" "$(echo "$F" | jf "'files' in d")" "True"
EV=$(curl -sS "$BASE/api/tasks/$TID/events" -H "$AUTH")
chk "events list shape" "$(echo "$EV" | jf "'events' in d")" "True"
S=$(curl -sS "$BASE/api/stats" -H "$AUTH")
chk "stats shape" "$(echo "$S" | jf "'aria2' in d and 'quota' in d")" "True"
SET=$(curl -sS "$BASE/api/settings" -H "$AUTH")
chk "settings shape" "$(echo "$SET" | jf "'download_dir' in d")" "True"

say "14. ownership isolation"
D=$(curl -sS -o "$TMP/d" -w "%{http_code}" "$BASE/api/tasks/$TID" -H "Authorization: Bearer $T3")
chk "other user's task -> 404" "$D" "404"

say "15. pause/cancel/retry state machine"
D=$(curl -sS -o "$TMP/d" -w "%{http_code}" -X POST "$BASE/api/tasks/$TID/cancel" -H "$AUTH")
chk "cancel -> 200" "$D" "200"
chk "  status" "$(cat "$TMP/d" | jf "d['task']['status']")" "canceled"
D=$(curl -sS -o "$TMP/d" -w "%{http_code}" -X POST "$BASE/api/tasks/$TID/start" -H "$AUTH")
chk "restart canceled -> 200" "$D" "200"
D=$(curl -sS -o "$TMP/d" -w "%{http_code}" -X POST "$BASE/api/tasks/$TID/pause" -H "$AUTH")
chk "pause -> 200" "$D" "200"
D=$(curl -sS -o "$TMP/d" -w "%{http_code}" -X POST "$BASE/api/tasks/$TID/resume" -H "$AUTH")
chk "resume -> 200" "$D" "200"
D=$(curl -sS -o "$TMP/d" -w "%{http_code}" -X POST "$BASE/api/tasks/$TID/retry" -H "$AUTH")
chk "retry -> 200" "$D" "200"

say "16. delete task"
D=$(curl -sS -o "$TMP/d" -w "%{http_code}" -X DELETE "$BASE/api/tasks/$TID" -H "$AUTH")
chk "delete -> 200" "$D" "200"
D=$(curl -sS -o "$TMP/d" -w "%{http_code}" "$BASE/api/tasks/$TID" -H "$AUTH")
chk "deleted -> 404" "$D" "404"

say "17. SPA fallback"
D=$(curl -sS -o "$TMP/d" -w "%{http_code}" "$BASE/app/tasks/1")
chk "SPA route serves html" "$D" "200"

printf '\n\033[1m==== %d passed, %d failed ====\033[0m\n' "$PASS" "$FAIL"
rm -rf "$TMP"
[ "$FAIL" -eq 0 ]
