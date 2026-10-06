#!/usr/bin/env bash
# Phase 14 — manual black-box probes against a running NEXUS binary.
# Targets what e2e/README.md records as "checked by hand": control
# metrics/components, division record read + transitions — plus a wire spot
# check of division narrowing, the error envelope and admission controls.
# It also probes the operational reliability surface by hand: the reliability
# discovery metadata, the capability lifecycle control plane, and one mutation
# whose remote outcome is deliberately lost (an `unknown` outcome must never be
# re-dispatched).
set -u

REPO=/home/pc/NEXUS
BIN=$REPO/e2e/.bin/nexus
DATA=$(mktemp -d /tmp/opencode/probe-XXXXXX)
BOOT="probe-boot-$(date +%s%N | tail -c 8)"
KEY="probe-control-$(date +%s%N | tail -c 8)"
HP=38110
GP=38111
BASE="http://127.0.0.1:$GP"
LOG=/tmp/opencode/probe-nexus.log
BIZ=default
DIV="div-probe-$$"
DIV2="div-probe2-$$"
SID="nx:human:probe-narrow-$$"
SID2="nx:human:probe-sibling-$$"
SCRED="probe-narrow-cred-$$"
S2CRED="probe-sibling-cred-$$"
PASS=0
FAIL=0

check() { # check <label> <expected> <actual>
  if [ "$2" = "$3" ]; then
    PASS=$((PASS + 1)); printf '  ok   %-56s %s\n' "$1" "$3"
  else
    FAIL=$((FAIL + 1)); printf '  FAIL %-56s want=%s got=%s\n' "$1" "$2" "$3"
  fi
}
contains() { # contains <label> <needle> <haystack>
  case "$3" in
    *"$2"*) PASS=$((PASS + 1)); printf '  ok   %-56s contains %s\n' "$1" "$2" ;;
    *)      FAIL=$((FAIL + 1)); printf '  FAIL %-56s missing %s\n' "$1" "$2" ;;
  esac
}
absent() { # absent <label> <needle> <haystack>
  case "$3" in
    *"$2"*) FAIL=$((FAIL + 1)); printf '  FAIL %-56s leaked %s\n' "$1" "$2" ;;
    *)      PASS=$((PASS + 1)); printf '  ok   %-56s absent\n' "$1" ;;
  esac
}

NEXUS_PID=""
FIXTURE_PID=""
cleanup() {
  [ -n "$FIXTURE_PID" ] && kill "$FIXTURE_PID" 2>/dev/null
  [ -n "$NEXUS_PID" ] && kill "$NEXUS_PID" 2>/dev/null
  wait 2>/dev/null
}
trap cleanup EXIT

echo "== boot =="
cd "$REPO" || exit 1
go build -o "$BIN" ./cmd/nexus || exit 1
NEXUS_DATA_DIR="$DATA" \
NEXUS_HEALTH_HOST=127.0.0.1 NEXUS_HEALTH_PORT="$HP" \
NEXUS_BOOTSTRAP_CREDENTIAL="$BOOT" NEXUS_BOOTSTRAP_BUSINESS="$BIZ" \
NEXUS_CONTROL_API_KEY="$KEY" NEXUS_LOG_FORMAT=json NEXUS_LOG_LEVEL=info \
NEXUS_ENVIRONMENT=development \
NEXUS_CAPABILITY_WEB=scripted NEXUS_SEEDED_PROVIDER_MODE=scripted \
NEXUS_TOOL_HTTP_ALLOW_INSECURE=true NEXUS_TOOL_HTTP_ALLOW_LOOPBACK=true \
NEXUS_TOOL_HTTP_ALLOWED_HOSTS=127.0.0.1 \
  "$BIN" >"$LOG" 2>&1 &
NEXUS_PID=$!

for _ in $(seq 1 300); do
  [ "$(curl -s -o /dev/null -w '%{http_code}' "$BASE/ready" 2>/dev/null)" = "200" ] && break
  kill -0 "$NEXUS_PID" 2>/dev/null || { echo "gateway died"; cat "$LOG"; exit 1; }
  sleep 0.1
done
echo "ready pid=$NEXUS_PID data=$DATA biz=$BIZ"

AUTH=(-H "X-Actor-ID: nx:human:bootstrap" -H "X-Actor-Credential: $BOOT" -H "Content-Type: application/json")
CTL=(-H "X-API-Key: $KEY" -H "Content-Type: application/json")

echo "== baseline =="
check "/health" 200 "$(curl -s -o /dev/null -w '%{http_code}' "$BASE/health")"
check "/ready" 200 "$(curl -s -o /dev/null -w '%{http_code}' "$BASE/ready")"
check "/status" 200 "$(curl -s -o /dev/null -w '%{http_code}' "$BASE/status")"

echo "== control status (uptime format) =="
sleep 2
ST=$(curl -s "${CTL[@]}" "$BASE/api/v1/control/status")
UP=$(echo "$ST" | jq -r .uptime)
check "control/status status" "RUNNING" "$(echo "$ST" | jq -r .status)"
case "$UP" in
  *s) check "control/status uptime is a Go duration" "1" "1" ;;
  *)  check "control/status uptime is a Go duration" "<...s>" "$UP" ;;
esac
contains "uptime advanced past 0s" "s" "$UP"
UP_NONZERO=false
[ "$UP" != "0s" ] && UP_NONZERO=true
check "uptime is not 0s after 2s" "true" "$UP_NONZERO"
check "request_count is a number" "number" "$(echo "$ST" | jq -r '.request_count|type')"
contains "components map is populated" "engine" "$ST"

echo "== control metrics / components (previously by-hand) =="
M=$(curl -s "${CTL[@]}" "$BASE/api/v1/control/metrics")
check "metrics.executor" "object" "$(echo "$M" | jq -r '.executor|type')"
check "metrics.backpressure" "object" "$(echo "$M" | jq -r '.backpressure|type')"
check "metrics.circuit_breaker.state" "string" "$(echo "$M" | jq -r '.circuit_breaker.state|type')"
check "metrics.recovery" "object" "$(echo "$M" | jq -r '.recovery|type')"
C=$(curl -s "${CTL[@]}" "$BASE/api/v1/control/components")
check "components.count >= 3" "true" "$(echo "$C" | jq -r '.count >= 3')"
check "components is an array" "array" "$(echo "$C" | jq -r '.components|type')"
check "components[0].name" "string" "$(echo "$C" | jq -r '.components[0].name|type')"
contains "components names the engine" "engine" "$(echo "$C" | jq -r '[.components[].name]|join(",")')"
check "control surface without key" 401 "$(curl -s -o /dev/null -w '%{http_code}' "$BASE/api/v1/control/status")"
check "control surface with wrong key" 401 "$(curl -s -o /dev/null -w '%{http_code}' -H 'X-API-Key: nope' "$BASE/api/v1/control/status")"

echo "== division record: create / read / transitions (previously by-hand) =="
DC=$(curl -s -o /tmp/opencode/probe-div.json -w '%{http_code}' "${AUTH[@]}" -X POST "$BASE/api/v1/divisions" \
  -d "{\"entity_id\":\"$DIV\",\"business_id\":\"$BIZ\",\"name\":\"Probe Division\",\"owner_identity_id\":\"nx:human:bootstrap\"}")
check "POST /divisions" 200 "$DC"
contains "created record echoes entity_id" "$DIV" "$(cat /tmp/opencode/probe-div.json)"
check "duplicate division create" 409 "$(curl -s -o /dev/null -w '%{http_code}' "${AUTH[@]}" -X POST "$BASE/api/v1/divisions" -d "{\"entity_id\":\"$DIV\",\"business_id\":\"$BIZ\",\"name\":\"again\",\"owner_identity_id\":\"nx:human:bootstrap\"}")"

check "GET /divisions?business_id=" 200 "$(curl -s -o /tmp/opencode/probe-divlist.json -w '%{http_code}' "${AUTH[@]}" "$BASE/api/v1/divisions?business_id=$BIZ")"
contains "list contains the new division" "$DIV" "$(cat /tmp/opencode/probe-divlist.json)"
check "GET /divisions without business_id" 400 "$(curl -s -o /dev/null -w '%{http_code}' "${AUTH[@]}" "$BASE/api/v1/divisions")"
check "GET /divisions/{id}" 200 "$(curl -s -o /tmp/opencode/probe-divget.json -w '%{http_code}' "${AUTH[@]}" "$BASE/api/v1/divisions/$DIV")"
contains "record carries business_id" "$BIZ" "$(cat /tmp/opencode/probe-divget.json)"
check "GET /divisions/{unknown}" 404 "$(curl -s -o /dev/null -w '%{http_code}' "${AUTH[@]}" "$BASE/api/v1/divisions/div-does-not-exist")"

check "POST /divisions/{id}/suspend" 200 "$(curl -s -o /tmp/opencode/probe-t1.json -w '%{http_code}' -X POST "${AUTH[@]}" "$BASE/api/v1/divisions/$DIV/suspend")"
check "  -> status suspended" "suspended" "$(jq -r .status /tmp/opencode/probe-t1.json)"
check "duplicate suspend" 409 "$(curl -s -o /dev/null -w '%{http_code}' -X POST "${AUTH[@]}" "$BASE/api/v1/divisions/$DIV/suspend")"
check "POST /divisions/{id}/activate" 200 "$(curl -s -o /tmp/opencode/probe-t2.json -w '%{http_code}' -X POST "${AUTH[@]}" "$BASE/api/v1/divisions/$DIV/activate")"
check "  -> status active" "active" "$(jq -r .status /tmp/opencode/probe-t2.json)"
check "duplicate activate" 409 "$(curl -s -o /dev/null -w '%{http_code}' -X POST "${AUTH[@]}" "$BASE/api/v1/divisions/$DIV/activate")"
# Archived is terminal (identity/orgTransitions: archived -> nil), so this
# runs on a throwaway division rather than the one the narrowing probes need.
DIVX="div-probe-archive-$$"
curl -s -o /dev/null "${AUTH[@]}" -X POST "$BASE/api/v1/divisions" \
  -d "{\"entity_id\":\"$DIVX\",\"business_id\":\"$BIZ\",\"name\":\"Probe Archived\",\"owner_identity_id\":\"nx:human:bootstrap\"}"
check "POST /divisions/{id}/archive" 200 "$(curl -s -o /tmp/opencode/probe-t3.json -w '%{http_code}' -X POST "${AUTH[@]}" "$BASE/api/v1/divisions/$DIVX/archive")"
check "  -> status archived" "archived" "$(jq -r .status /tmp/opencode/probe-t3.json)"
check "archived is terminal (reactivate -> 409)" 409 "$(curl -s -o /dev/null -w '%{http_code}' -X POST "${AUTH[@]}" "$BASE/api/v1/divisions/$DIVX/activate")"
check "archived is terminal (re-archive -> 409)" 409 "$(curl -s -o /dev/null -w '%{http_code}' -X POST "${AUTH[@]}" "$BASE/api/v1/divisions/$DIVX/archive")"
check "unregistered transition path" 404 "$(curl -s -o /dev/null -w '%{http_code}' -X POST "${AUTH[@]}" "$BASE/api/v1/divisions/$DIV/destroy")"
check "POST /divisions/{unknown}/suspend" 404 "$(curl -s -o /dev/null -w '%{http_code}' -X POST "${AUTH[@]}" "$BASE/api/v1/divisions/div-does-not-exist/suspend")"

echo "== division narrowing on the wire =="
curl -s -o /dev/null "${AUTH[@]}" -X POST "$BASE/api/v1/divisions" \
  -d "{\"entity_id\":\"$DIV2\",\"business_id\":\"$BIZ\",\"name\":\"Probe Sibling\",\"owner_identity_id\":\"nx:human:bootstrap\"}"
IC=$(curl -s -o /tmp/opencode/probe-id1.json -w '%{http_code}' "${AUTH[@]}" -X POST "$BASE/api/v1/identities" \
  -d "{\"identity_type\":\"human\",\"entity_id\":\"$SID\",\"display_name\":\"narrow\",\"business_id\":\"$BIZ\",\"division_id\":\"$DIV\",\"credential\":\"$SCRED\",\"credential_method\":\"password\"}")
check "create division-scoped identity" 200 "$IC"
absent "raw credential never echoed" "$SCRED" "$(cat /tmp/opencode/probe-id1.json)"
curl -s -o /dev/null "${AUTH[@]}" -X POST "$BASE/api/v1/identities" \
  -d "{\"identity_type\":\"human\",\"entity_id\":\"$SID2\",\"display_name\":\"sibling\",\"business_id\":\"$BIZ\",\"division_id\":\"$DIV2\",\"credential\":\"$S2CRED\",\"credential_method\":\"password\"}"

NAR=(-H "X-Actor-ID: $SID" -H "X-Actor-Credential: $SCRED" -H "Content-Type: application/json")
SIB=(-H "X-Actor-ID: $SID2" -H "X-Actor-Credential: $S2CRED" -H "Content-Type: application/json")

SC=$(curl -s -o /tmp/opencode/probe-sub.json -w '%{http_code}' "${NAR[@]}" -X POST "$BASE/api/v1/requests" \
  -d "{\"intent\":\"probe division scope\",\"business_id\":\"$BIZ\",\"division_id\":\"$DIV\",\"actor_id\":\"$SID\"}")
check "submit with division_id" 202 "$SC"
RID=$(jq -r .request_id /tmp/opencode/probe-sub.json)
contains "submit echoes status accepted" "accepted" "$(cat /tmp/opencode/probe-sub.json)"

BAD=$(curl -s -o /tmp/opencode/probe-bad.json -w '%{http_code}' "${NAR[@]}" -X POST "$BASE/api/v1/requests" \
  -d "{\"intent\":\"unknown division\",\"business_id\":\"$BIZ\",\"division_id\":\"div-nope\",\"actor_id\":\"$SID\"}")
check "submit with an unknown division" 400 "$BAD"
contains "  -> validation message" "division not found" "$(cat /tmp/opencode/probe-bad.json)"
check "  -> no request id issued" "false" "$(jq -r 'has("request_id")' /tmp/opencode/probe-bad.json)"

CODE=000
for _ in $(seq 1 300); do
  CODE=$(curl -s -o /tmp/opencode/probe-res.json -w '%{http_code}' "${NAR[@]}" "$BASE/api/v1/requests/$RID?business_id=$BIZ")
  [ "$CODE" = "200" ] && break
  sleep 0.1
done
check "GET result reaches 200" 200 "$CODE"
check "result echoes the recorded division" "$DIV" "$(jq -r .division_id /tmp/opencode/probe-res.json)"
check "result echoes the business" "$BIZ" "$(jq -r .business_id /tmp/opencode/probe-res.json)"

check "business-wide member reads the divisional record" 200 \
  "$(curl -s -o /dev/null -w '%{http_code}' "${AUTH[@]}" "$BASE/api/v1/requests/$RID?business_id=$BIZ")"
SR=$(curl -s -o /tmp/opencode/probe-sibread.json -w '%{http_code}' "${SIB[@]}" "$BASE/api/v1/requests/$RID?business_id=$BIZ")
check "sibling division is invisible on read (G5)" 404 "$SR"
contains "  -> documented message" '"message":"request not found"' "$(cat /tmp/opencode/probe-sibread.json)"
check "refusal is an error envelope, not a result" "object" "$(jq -r '.error|type' /tmp/opencode/probe-sibread.json)"

# A second request carries the cancel probes, so the terminal-state branch
# cannot mask the division branch. The engine dispatches serially, so a burst
# of admissions keeps the target queued long enough to cancel deterministically.
FILL_PIDS=()
for i in $(seq 1 40); do
  curl -s -m 5 -o /dev/null "${AUTH[@]}" -X POST "$BASE/api/v1/requests" \
    -d "{\"intent\":\"probe fill $i\",\"business_id\":\"$BIZ\",\"actor_id\":\"nx:human:bootstrap\"}" &
  FILL_PIDS+=($!)
done
for p in "${FILL_PIDS[@]}"; do wait "$p"; done
SC2=$(curl -s -o /tmp/opencode/probe-sub2.json -w '%{http_code}' "${NAR[@]}" -X POST "$BASE/api/v1/requests" \
  -d "{\"intent\":\"probe cancel narrowing\",\"business_id\":\"$BIZ\",\"division_id\":\"$DIV\",\"actor_id\":\"$SID\"}")
check "second submit" 202 "$SC2"
RID2=$(jq -r .request_id /tmp/opencode/probe-sub2.json)
# The owner's cancel goes first so the race window is as short as possible;
# the sibling's cancel is checked afterwards and must still be refused,
# because core.CancelRequest narrows on the recorded division before it
# reports a terminal state.
check "own division cancels" 202 "$(curl -s -o /dev/null -w '%{http_code}' "${NAR[@]}" -X POST "$BASE/api/v1/requests/$RID2/cancel?business_id=$BIZ")"
XC=$(curl -s -o /tmp/opencode/probe-sibcancel.json -w '%{http_code}' "${SIB[@]}" -X POST "$BASE/api/v1/requests/$RID2/cancel?business_id=$BIZ")
check "sibling division is invisible on cancel (G5)" 404 "$XC"
contains "  -> documented message" '"message":"request not found"' "$(cat /tmp/opencode/probe-sibcancel.json)"
C2=000
for _ in $(seq 1 300); do
  C2=$(curl -s -o /tmp/opencode/probe-r2.json -w '%{http_code}' "${NAR[@]}" "$BASE/api/v1/requests/$RID2?business_id=$BIZ")
  [ "$C2" = "200" ] && break
  sleep 0.1
done
check "cancelled request reaches 200" 200 "$C2"
check "  -> status cancelled" "cancelled" "$(jq -r .status /tmp/opencode/probe-r2.json)"
check "  -> cancelled result keeps the division" "$DIV" "$(jq -r .division_id /tmp/opencode/probe-r2.json)"

echo "== G3/G2/G5 wire posture =="
# G3: a division-scoped membership never submits at business scope.
NW=$(curl -s -o /tmp/opencode/probe-narwide.json -w '%{http_code}' "${NAR[@]}" -X POST "$BASE/api/v1/requests" \
  -d "{\"intent\":\"narrow business-scope submit\",\"business_id\":\"$BIZ\",\"actor_id\":\"$SID\"}")
check "narrow actor at business scope is 403" 403 "$NW"
contains "  -> documented message" "business-scope requests require a business-wide membership" "$(cat /tmp/opencode/probe-narwide.json)"
# G3+G5: a business-level record is invisible to a division member (404).
W3=$(curl -s -o /tmp/opencode/probe-plain.json -w '%{http_code}' "${AUTH[@]}" -X POST "$BASE/api/v1/requests" \
  -d "{\"intent\":\"business-scope probe\",\"business_id\":\"$BIZ\",\"actor_id\":\"nx:human:bootstrap\"}")
check "business-level submit" 202 "$W3"
RP=$(jq -r .request_id /tmp/opencode/probe-plain.json)
sleep 2
NR=$(curl -s -o /tmp/opencode/probe-narread.json -w '%{http_code}' "${NAR[@]}" "$BASE/api/v1/requests/$RP?business_id=$BIZ")
check "division member reading business-level record gets 404" 404 "$NR"
# G2: suspending the business closes admission; reads still work; activate reopens.
curl -s -o /dev/null -X POST "$BASE/api/v1/businesses/$BIZ/suspend" -H "X-Actor-ID: nx:human:bootstrap" -H "X-Actor-Credential: $BOOT" -H 'Content-Type: application/json' -d '{}'
SB=$(curl -s -o /tmp/opencode/probe-suspbiz.json -w '%{http_code}' "${NAR[@]}" -X POST "$BASE/api/v1/requests" \
  -d "{\"intent\":\"blocked\",\"business_id\":\"$BIZ\",\"division_id\":\"$DIV\",\"actor_id\":\"$SID\"}")
check "suspended business blocks submit (409)" 409 "$SB"
contains "  -> documented message" "no new work is admitted" "$(cat /tmp/opencode/probe-suspbiz.json)"
RB=$(curl -s -o /dev/null -w '%{http_code}' "${AUTH[@]}" "$BASE/api/v1/requests/$RID2?business_id=$BIZ")
check "existing record still readable while suspended" 200 "$RB"
curl -s -o /dev/null -X POST "$BASE/api/v1/businesses/$BIZ/activate" -H "X-Actor-ID: nx:human:bootstrap" -H "X-Actor-Credential: $BOOT" -H 'Content-Type: application/json' -d '{}'
SBO=$(curl -s -o /dev/null -w '%{http_code}' "${NAR[@]}" -X POST "$BASE/api/v1/requests" \
  -d "{\"intent\":\"reopened\",\"business_id\":\"$BIZ\",\"division_id\":\"$DIV\",\"actor_id\":\"$SID\"}")
check "activate reopens admission" 202 "$SBO"

echo "== policy control lifecycle on the wire =="
check "seeded default-allow GET" 200 "$(curl -s -o /dev/null -w '%{http_code}' "${CTL[@]}" "$BASE/api/v1/control/policies/default-allow")"
check "seeded default-allow PUT" 409 "$(curl -s -o /dev/null -w '%{http_code}' -X PUT "${CTL[@]}" "$BASE/api/v1/control/policies/default-allow" -d '{"policy_id":"default-allow","effect":"DENY","name":"x","description":"x","status":"active","policy_type":"custom","subject":{"subject_type":"all"},"action":{"action_type":"custom"},"resource":{"resource_type":"all"}}')"
check "seeded default-allow DELETE" 409 "$(curl -s -o /dev/null -w '%{http_code}' -X DELETE "${CTL[@]}" "$BASE/api/v1/control/policies/default-allow")"
PID="e2e-probe-$$"
PBODY="{\"policy_id\":\"$PID\",\"effect\":\"DENY\",\"name\":\"probe\",\"description\":\"probe\",\"status\":\"active\",\"policy_type\":\"custom\",\"subject\":{\"subject_type\":\"all\"},\"action\":{\"action_type\":\"custom\"},\"resource\":{\"resource_type\":\"all\"},\"precedence\":7}"
check "PUT creates" 200 "$(curl -s -o /dev/null -w '%{http_code}' -X PUT "${CTL[@]}" "$BASE/api/v1/control/policies/$PID" -d "$PBODY")"
check "GET reads it back" 200 "$(curl -s -o /dev/null -w '%{http_code}' "${CTL[@]}" "$BASE/api/v1/control/policies/$PID")"
PBODY2=$(echo "$PBODY" | sed "s/\"effect\":\"DENY\"/\"effect\":\"ALLOW\"/")
check "PUT replaces the same id" 200 "$(curl -s -o /dev/null -w '%{http_code}' -X PUT "${CTL[@]}" "$BASE/api/v1/control/policies/$PID" -d "$PBODY2")"
check "  -> replacement readable" "ALLOW" "$(curl -s "${CTL[@]}" "$BASE/api/v1/control/policies/$PID" | jq -r .effect)"
check "DELETE removes it" 200 "$(curl -s -o /dev/null -w '%{http_code}' -X DELETE "${CTL[@]}" "$BASE/api/v1/control/policies/$PID")"
check "GET after delete" 404 "$(curl -s -o /dev/null -w '%{http_code}' "${CTL[@]}" "$BASE/api/v1/control/policies/$PID")"
check "DELETE again" 404 "$(curl -s -o /dev/null -w '%{http_code}' -X DELETE "${CTL[@]}" "$BASE/api/v1/control/policies/$PID")"
check "GET /control/policies (list)" 200 "$(curl -s -o /dev/null -w '%{http_code}' "${CTL[@]}" "$BASE/api/v1/control/policies")"

echo "== error envelope on the wire =="
check "unrouted path" 404 "$(curl -s -o /dev/null -w '%{http_code}' "${AUTH[@]}" "$BASE/api/v1/nope")"
EHDR=$(curl -s -D - -o /tmp/opencode/probe-err.json "${AUTH[@]}" -H "X-Correlation-ID: probe-corr-1" "$BASE/api/v1/nope")
contains "unrouted message names method and path" "no such endpoint: GET /api/v1/nope" "$(cat /tmp/opencode/probe-err.json)"
check "correlation_id honours X-Correlation-ID" "probe-corr-1" "$(jq -r .error.correlation_id /tmp/opencode/probe-err.json)"
contains "response header matches body" "x-correlation-id: probe-corr-1" "$(echo "$EHDR" | tr 'A-Z' 'a-z')"
check "404 code is VALIDATION (no NOT_FOUND category)" "VALIDATION" "$(jq -r .error.code /tmp/opencode/probe-err.json)"
check "404 category is VALIDATION" "VALIDATION" "$(jq -r .error.category /tmp/opencode/probe-err.json)"
check "envelope carries every required field" "true" \
  "$(jq -r '(.error|has("code")) and (.error|has("category")) and (.error|has("message")) and (.error|has("retryable")) and (.error|has("timestamp")) and (.error|has("correlation_id"))' /tmp/opencode/probe-err.json)"
check "wrong method" 405 "$(curl -s -o /dev/null -w '%{http_code}' -X DELETE "${AUTH[@]}" "$BASE/api/v1/requests")"
check "missing business_id on read" 400 "$(curl -s -o /dev/null -w '%{http_code}' "${AUTH[@]}" "$BASE/api/v1/requests/req-1?business_id=")"
check "no credentials with enforcement on" 401 "$(curl -s -o /dev/null -w '%{http_code}' "$BASE/api/v1/requests/req-1?business_id=$BIZ")"
check "wrong credential" 401 "$(curl -s -o /dev/null -w '%{http_code}' -H 'X-Actor-ID: nx:human:bootstrap' -H 'X-Actor-Credential: wrong' "$BASE/api/v1/requests/req-1?business_id=$BIZ")"

echo "== capability & tool platform =="
# The manifests endpoint is membership-bound (scoped API path) and lists
# the registered tool manifests — informational catalog, no secrets.
T=$(curl -s -o /tmp/opencode/probe-tools.json -w '%{http_code}' "${AUTH[@]}" "$BASE/api/v1/tools?business_id=$BIZ")
check "GET /api/v1/tools" 200 "$T"
contains "catalog lists http.request" '"http.request"' "$(jq -c '[.tools[].tool_id]' /tmp/opencode/probe-tools.json)"
contains "catalog lists filesystem.read" '"filesystem.read"' "$(jq -c '[.tools[].tool_id]' /tmp/opencode/probe-tools.json)"
contains "catalog lists git" '"git"' "$(jq -c '[.tools[].tool_id]' /tmp/opencode/probe-tools.json)"
contains "catalog lists web.search" '"web.search"' "$(jq -c '[.tools[].tool_id]' /tmp/opencode/probe-tools.json)"
contains "catalog lists data" '"data"' "$(jq -c '[.tools[].tool_id]' /tmp/opencode/probe-tools.json)"
contains "catalog declares side_effect_class" '"side_effect_class"' "$(cat /tmp/opencode/probe-tools.json)"
contains "catalog declares supported_operations" '"supported_operations"' "$(cat /tmp/opencode/probe-tools.json)"
NOBUS=$(curl -s -o /dev/null -w '%{http_code}' "${AUTH[@]}" "$BASE/api/v1/tools")
check "GET /api/v1/tools missing business_id -> 400" 400 "$NOBUS"
NOAUTH=$(curl -s -o /dev/null -w '%{http_code}' "$BASE/api/v1/tools?business_id=$BIZ")
check "GET /api/v1/tools without auth" 401 "$NOAUTH"
FOREIGN=$(curl -s -o /dev/null -w '%{http_code}' "${AUTH[@]}" "$BASE/api/v1/tools?business_id=other-biz-$RANDOM")
check "GET /api/v1/tools foreign business -> 403" 403 "$FOREIGN"
CONTENT=$(cat /tmp/opencode/probe-tools.json)
absent "catalog carries no secrets" 'Bearer ' "$CONTENT"
contains "catalog marks http security_class network" '"network"' "$(jq -c '[.tools[]|select(.tool_id=="http.request")|.security_class]' /tmp/opencode/probe-tools.json)"


echo "== operational reliability & tool semantics =="
# Discovery states the reliability posture of each capability. It is
# informational: it grants nothing and reveals no topology or credentials.
REL=$(jq -c '.tools[]|select(.tool_id=="http.request")|{capability_state,supports_idempotency,supports_reconciliation,retry_policy,max_attempts}' /tmp/opencode/probe-tools.json)
contains "discovery reports capability_state" '"capability_state":"enabled"' "$REL"
contains "discovery reports idempotency support" '"supports_idempotency":true' "$REL"
contains "discovery advertises no reconciliation it does not implement" '"supports_reconciliation":false' "$REL"
contains "discovery reports the retry policy" '"retry_policy":"not_sent_retry"' "$REL"
contains "discovery reports the attempt bound" '"max_attempts":2' "$REL"
contains "a declared read capability reports the read retry policy" '"retry_policy":"bounded_read_retry"' \
  "$(jq -c '.tools[]|select(.tool_id=="git")|{side_effect_class,retry_policy,max_attempts}' /tmp/opencode/probe-tools.json)"

# Lifecycle control is an operator action under the control prefix: no key and
# a wrong key are both refused before any handler runs.
check "lifecycle without a key" 401 "$(curl -s -o /dev/null -w '%{http_code}' -X POST \
  -H 'Content-Type: application/json' -d '{"state":"disabled"}' "$BASE/api/v1/control/capabilities/http.request/state")"
check "lifecycle with a wrong key" 401 "$(curl -s -o /dev/null -w '%{http_code}' -X POST \
  -H 'X-API-Key: not-the-key' -H 'Content-Type: application/json' -d '{"state":"disabled"}' \
  "$BASE/api/v1/control/capabilities/http.request/state")"
check "lifecycle on an unregistered capability" 404 "$(curl -s -o /dev/null -w '%{http_code}' "${CTL[@]}" -X POST \
  -d '{"state":"disabled"}' "$BASE/api/v1/control/capabilities/not.registered/state")"
check "lifecycle with an unknown state" 400 "$(curl -s -o /dev/null -w '%{http_code}' "${CTL[@]}" -X POST \
  -d '{"state":"sideways"}' "$BASE/api/v1/control/capabilities/http.request/state")"
check "disable a capability" "disabled" "$(curl -s "${CTL[@]}" -X POST -d '{"state":"disabled"}' \
  "$BASE/api/v1/control/capabilities/http.request/state" | jq -r .capability_state)"
check "catalog reports the new state" "disabled" "$(curl -s "${AUTH[@]}" "$BASE/api/v1/tools?business_id=$BIZ" \
  | jq -r '.tools[]|select(.tool_id=="http.request")|.capability_state')"
check "disabled -> deprecated is outside the graph" 409 "$(curl -s -o /dev/null -w '%{http_code}' "${CTL[@]}" -X POST \
  -d '{"state":"deprecated"}' "$BASE/api/v1/control/capabilities/http.request/state")"
check "disabled -> deprecated leaves the state alone" "disabled" "$(curl -s "${AUTH[@]}" "$BASE/api/v1/tools?business_id=$BIZ" \
  | jq -r '.tools[]|select(.tool_id=="http.request")|.capability_state')"
check "re-enable the capability" "enabled" "$(curl -s "${CTL[@]}" -X POST -d '{"state":"enabled"}' \
  "$BASE/api/v1/control/capabilities/http.request/state" | jq -r .capability_state)"
check "enabled -> deprecated" "deprecated" "$(curl -s "${CTL[@]}" -X POST -d '{"state":"deprecated"}' \
  "$BASE/api/v1/control/capabilities/http.request/state" | jq -r .capability_state)"
check "deprecated still executes (re-enable)" "enabled" "$(curl -s "${CTL[@]}" -X POST -d '{"state":"enabled"}' \
  "$BASE/api/v1/control/capabilities/http.request/state" | jq -r .capability_state)"

# A mutation whose response is lost has an UNKNOWN outcome: the objective fails,
# the outcome stays distinguishable, and the remote mutation happens exactly
# once — no retry, no duplicate.
if command -v python3 >/dev/null 2>&1; then
  FIXPORT=38220
  FIXLOG=/tmp/opencode/probe-fixture.log
  : >"$FIXLOG"
  python3 - "$FIXPORT" "$FIXLOG" <<'PY' &
import socket, sys, threading
port, log = int(sys.argv[1]), sys.argv[2]
def handle(conn):
    try:
        data = conn.recv(65536)
        parts = data.split(b" ")
        with open(log, "a") as fh:
            fh.write(parts[0].decode() + " " + (parts[1] if len(parts) > 1 else "") .decode() + "\n")
        if len(parts) > 1 and parts[1].startswith(b"/lost-response"):
            conn.close()  # dispatched, response never observed
            return
        body = b"nexus-probe-fixture"
        conn.sendall(b"HTTP/1.1 200 OK\r\nContent-Type: text/plain\r\nContent-Length: "
                     + str(len(body)).encode() + b"\r\nConnection: close\r\n\r\n" + body)
        conn.close()
    except Exception:
        pass
srv = socket.socket()
srv.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
srv.bind(("127.0.0.1", port))
srv.listen(16)
while True:
    c, _ = srv.accept()
    threading.Thread(target=handle, args=(c,), daemon=True).start()
PY
  FIXTURE_PID=$!
  sleep 0.3
  RAGENT="probe-reliability-$$"
  curl -s -o /dev/null "${AUTH[@]}" -X POST "$BASE/api/v1/agents" \
    -d "{\"entity_id\":\"$RAGENT\",\"name\":\"Probe Reliability\",\"business_id\":\"$BIZ\",\"capabilities\":[\"analysis\"],\"allowed_tools\":[\"http.request\"]}"
  SUB=$(curl -s "${AUTH[@]}" -X POST "$BASE/api/v1/intelligence/execute" \
    -d "{\"business_id\":\"$BIZ\",\"actor_id\":\"nx:human:bootstrap\",\"objective\":{\"description\":\"post via http request to http://127.0.0.1:$FIXPORT/lost-response\"}}")
  EID=$(echo "$SUB" | jq -r .execution_id)
  RES=""
  for _ in $(seq 1 600); do
    RES=$(curl -s "${AUTH[@]}" "$BASE/api/v1/intelligence/$EID?business_id=$BIZ")
    ST=$(echo "$RES" | jq -r .status)
    case "$ST" in completed|failed|cancelled) break ;; esac
    sleep 0.1
  done
  check "an indeterminate mutation fails the objective" "failed" "$(echo "$RES" | jq -r .status)"
  contains "the unknown outcome stays distinguishable" "unknown outcome" \
    "$(echo "$RES" | jq -r '.error.message // .outcome.summary')"
  check "the remote mutation happened exactly once" "1" "$(grep -c '^POST /lost-response' "$FIXLOG")"
  check "no read was issued for it" "0" "$(grep -c '^GET /lost-response' "$FIXLOG")"
else
  echo "  skip python3 unavailable: the unknown-outcome wire probe did not run"
fi

echo "== pause / resume / readiness =="
check "pause" 200 "$(curl -s -o /dev/null -w '%{http_code}' -X POST "${CTL[@]}" "$BASE/api/v1/control/pause")"
check "ready while paused" 503 "$(curl -s -o /dev/null -w '%{http_code}' "$BASE/ready")"
check "submit while paused" 503 "$(curl -s -o /dev/null -w '%{http_code}' "${NAR[@]}" -X POST "$BASE/api/v1/requests" -d "{\"intent\":\"while paused\",\"business_id\":\"$BIZ\",\"division_id\":\"$DIV\",\"actor_id\":\"$SID\"}")"
check "pause again" 409 "$(curl -s -o /dev/null -w '%{http_code}' -X POST "${CTL[@]}" "$BASE/api/v1/control/pause")"
check "resume" 200 "$(curl -s -o /dev/null -w '%{http_code}' -X POST "${CTL[@]}" "$BASE/api/v1/control/resume")"
check "ready after resume" 200 "$(curl -s -o /dev/null -w '%{http_code}' "$BASE/ready")"
check "resume again" 409 "$(curl -s -o /dev/null -w '%{http_code}' -X POST "${CTL[@]}" "$BASE/api/v1/control/resume")"

echo
echo "PASS=$PASS FAIL=$FAIL"
echo "log=$LOG data=$DATA"
[ "$FAIL" -eq 0 ]
