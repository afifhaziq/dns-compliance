#!/usr/bin/env bash
# dns_benchmark.sh — Baseline DNS query benchmark using dig, nslookup, and curl (DoH).
# Compare results against the dns-compliance crawler to measure concurrency gains.
#
# Usage:
#   ./dns_benchmark.sh [site-list.txt] [dns-server.yaml]
#
# Semantics (mirrors crawler):
#   resolved → VIOLATION  (ISP takedown failed — site still reachable)
#   nxdomain → COMPLIANT  (ISP takedown working — site blocked)
#
# Output:
#   benchmark_results/benchmark_<timestamp>.txt  — human-readable report
#   benchmark_results/benchmark_<timestamp>.csv  — machine-readable data
#
# Tools used per protocol:
#   udp/dot  → dig  +  nslookup  (both run per site)
#   doh      → dig +https  +  curl (JSON API)  (both run per site)

set -euo pipefail

SITE_LIST="${1:-site-list.txt}"
DNS_YAML="${2:-dns-server.yaml}"
OUTPUT_DIR="benchmark_results"
TIMESTAMP=$(date +"%Y%m%d_%H%M%S")
REPORT_FILE="${OUTPUT_DIR}/benchmark_${TIMESTAMP}.txt"
CSV_FILE="${OUTPUT_DIR}/benchmark_${TIMESTAMP}.csv"

# ── ANSI colors (terminal only) ───────────────────────────────────────────────
RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'
BLUE='\033[0;34m'; BOLD='\033[1m'; NC='\033[0m'

# ── Helpers ───────────────────────────────────────────────────────────────────

die()    { echo "ERROR: $*" >&2; exit 1; }
now_ms() { date +%s%3N; }

# Print a line to both terminal (with ANSI) and plain-text report file.
log() {
    echo -e "$*"
    echo -e "$*" | sed 's/\x1B\[[0-9;]*[mGKHF]//g' >> "$REPORT_FILE"
}

# Print a formatted row to terminal (colored) and to the report file (plain).
log_row() {
    local color="$1" fmt="$2"; shift 2
    printf "${color}${fmt}${NC}" "$@"
    # shellcheck disable=SC2059
    printf "$fmt" "$@" >> "$REPORT_FILE"
}

# ── Preflight checks ──────────────────────────────────────────────────────────

[[ -f "$SITE_LIST" ]] || die "Site list not found: $SITE_LIST"
[[ -f "$DNS_YAML"  ]] || die "DNS config not found: $DNS_YAML"
command -v dig      >/dev/null 2>&1 || die "'dig' not found — install bind-utils / dnsutils"
command -v nslookup >/dev/null 2>&1 || die "'nslookup' not found — install bind-utils / dnsutils"
command -v curl     >/dev/null 2>&1 || die "'curl' not found"
command -v bc       >/dev/null 2>&1 || die "'bc' not found"

mkdir -p "$OUTPUT_DIR"

# ── Parse site list ───────────────────────────────────────────────────────────
# Strip blank lines, strip URL scheme and path, deduplicate.

mapfile -t SITES < <(
    grep -v '^\s*$' "$SITE_LIST" \
    | sed 's|https\?://||; s|/.*||' \
    | awk 'NF' \
    | sort -u
)
NUM_SITES=${#SITES[@]}
[[ $NUM_SITES -gt 0 ]] || die "No sites parsed from $SITE_LIST"

# ── Parse DNS servers from YAML (no external parser) ─────────────────────────
# Handles: name, address, protocol fields under the servers list.

declare -a SRV_NAME SRV_ADDR SRV_PROTO
idx=-1

while IFS= read -r line; do
    trimmed="${line#"${line%%[![:space:]]*}"}"   # strip leading whitespace
    case "$trimmed" in
        "- name: "*)   idx=$((idx+1)); SRV_NAME[$idx]="${trimmed#*name: }" ;;
        "address: "*)  [[ $idx -ge 0 ]] && SRV_ADDR[$idx]="${trimmed#*address: }" ;;
        "protocol: "*) [[ $idx -ge 0 ]] && SRV_PROTO[$idx]="${trimmed#*protocol: }" ;;
    esac
done < "$DNS_YAML"

NUM_SERVERS=${#SRV_NAME[@]}
[[ $NUM_SERVERS -gt 0 ]] || die "No DNS servers parsed from $DNS_YAML"

# ── Query functions ───────────────────────────────────────────────────────────
# Each returns a pipe-separated string: "resolved|<ip>|<ms>" or "nxdomain||<ms>"

# Plain UDP/TCP dig query.
query_dig() {
    local ip="$1" port="$2" site="$3"
    local t0 t1 out ip_result
    t0=$(now_ms)
    out=$(dig +short +time=5 +tries=1 @"$ip" -p "$port" "$site" A 2>/dev/null || true)
    t1=$(now_ms)
    ip_result=$(grep -Eo '^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$' <<< "$out" | head -1 || true)
    if [[ -n "$ip_result" ]]; then
        echo "resolved|${ip_result}|$((t1-t0))"
    else
        echo "nxdomain||$((t1-t0))"
    fi
}

# nslookup UDP query.
query_nslookup() {
    local ip="$1" site="$2"
    local t0 t1 out ip_result
    t0=$(now_ms)
    out=$(nslookup -timeout=5 -retry=1 "$site" "$ip" 2>&1 || true)
    t1=$(now_ms)
    # "Address:" lines without "#port" are resolved IPs (server header has "#53").
    ip_result=$(grep -E "^Address: [0-9]" <<< "$out" | grep -v '#' | awk '{print $2}' | head -1 || true)
    if [[ -n "$ip_result" ]]; then
        echo "resolved|${ip_result}|$((t1-t0))"
    else
        echo "nxdomain||$((t1-t0))"
    fi
}

# DoH via dig +https (requires dig >= 9.18; extracts host IP from the endpoint URL).
query_dig_doh() {
    local endpoint="$1" site="$2"
    local t0 t1 out ip_result host_ip
    # Extract host IP from "https://<ip>/..."
    host_ip=$(echo "$endpoint" | grep -oE '[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+' | head -1)
    t0=$(now_ms)
    out=$(dig +short +https +time=5 +tries=1 @"${host_ip}" "$site" A 2>/dev/null || true)
    t1=$(now_ms)
    ip_result=$(grep -Eo '^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$' <<< "$out" | head -1 || true)
    if [[ -n "$ip_result" ]]; then
        echo "resolved|${ip_result}|$((t1-t0))"
    else
        echo "nxdomain||$((t1-t0))"
    fi
}

# DoH via curl with JSON API.
# Google's /dns-query is RFC 8484 wire format only — JSON lives at /resolve.
# Cloudflare's /dns-query supports JSON when Accept: application/dns-json is set.
doh_json_url() {
    local endpoint="$1" site="$2"
    case "$endpoint" in
        *8.8.8.8*|*dns.google*)
            # Google JSON API is at /resolve, not /dns-query
            echo "${endpoint/dns-query/resolve}?name=${site}&type=A"
            ;;
        *)
            # Cloudflare and others accept application/dns-json at /dns-query
            echo "${endpoint}?name=${site}&type=A"
            ;;
    esac
}

query_curl_doh() {
    local endpoint="$1" site="$2"
    local t0 t1 out ip_result json_url
    json_url=$(doh_json_url "$endpoint" "$site")
    t0=$(now_ms)
    out=$(curl -sf --max-time 5 \
        -H "Accept: application/dns-json" \
        "$json_url" 2>/dev/null || true)
    t1=$(now_ms)
    # Extract IP from "data":"<ip>" in JSON Answer array
    ip_result=$(echo "$out" \
        | grep -oE '"data":"[^"]+"' \
        | grep -oE '[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+' \
        | head -1 || true)
    if [[ -n "$ip_result" ]]; then
        echo "resolved|${ip_result}|$((t1-t0))"
    else
        echo "nxdomain||$((t1-t0))"
    fi
}

# ── Initialize report and CSV ─────────────────────────────────────────────────

{
    echo "DNS Compliance Benchmark — Baseline (dig / nslookup / curl)"
    echo "============================================================"
    echo "Generated : $(date)"
    echo "Site list : $SITE_LIST  ($NUM_SITES sites)"
    echo "DNS config: $DNS_YAML  ($NUM_SERVERS servers)"
    echo ""
    echo "Semantics: resolved -> VIOLATION (ISP takedown failed)"
    echo "           nxdomain -> COMPLIANT (ISP takedown working)"
    echo ""
} > "$REPORT_FILE"

echo "server,site,tool,status,ip,time_ms,verdict" > "$CSV_FILE"

# ── Print header ──────────────────────────────────────────────────────────────

DIG_VER=$(dig -v 2>&1 | grep -oE 'DiG [0-9.]+' || echo "dig")

log "${BOLD}DNS Compliance Benchmark — Baseline${NC}"
log "====================================="
log "Site list  : $SITE_LIST  (${NUM_SITES} sites)"
log "DNS config : $DNS_YAML  (${NUM_SERVERS} servers)"
log "dig version: ${DIG_VER}"
log ""
log "${BOLD}DNS Servers:${NC}"
for ((i=0; i<NUM_SERVERS; i++)); do
    log "  [${i}] ${SRV_NAME[$i]}  —  ${SRV_ADDR[$i]}  (${SRV_PROTO[$i]})"
done
log ""
log "Tools: UDP → dig + nslookup  |  DoH → dig +https + curl (JSON API)"
log "Mode : sequential (one query at a time — compare vs concurrent crawler)"
log ""

# ── Main benchmark loop ───────────────────────────────────────────────────────

ROW_FMT="%-40s %-14s %-10s %-18s %-10s %s\n"
SEP="$(printf '%0.s-' {1..108})"

TOTAL_VIOLATION=0
TOTAL_COMPLIANT=0
TOTAL_QUERIES=0
GLOBAL_START=$(now_ms)

for ((si=0; si<NUM_SERVERS; si++)); do
    srv_name="${SRV_NAME[$si]}"
    srv_addr="${SRV_ADDR[$si]}"
    srv_proto="${SRV_PROTO[$si]}"

    log ""
    log "${BOLD}${BLUE}=== ${srv_name}  |  ${srv_addr}  |  ${srv_proto^^} ===${NC}"

    SRV_VIOLATION=0; SRV_COMPLIANT=0
    SRV_START=$(now_ms)

    # Select query functions and display names based on protocol
    if [[ "$srv_proto" == "doh" ]]; then
        tool_keys=("dig_doh" "curl_doh")
        tool_labels=("dig+https" "curl(DoH)")
    else
        # Extract host IP and port for UDP queries
        srv_ip="${srv_addr%:*}"
        srv_port="${srv_addr##*:}"
        [[ "$srv_port" == "$srv_ip" ]] && srv_port="53"
        tool_keys=("dig" "nslookup")
        tool_labels=("dig" "nslookup")
    fi

    for ti in "${!tool_keys[@]}"; do
        tool="${tool_keys[$ti]}"
        label="${tool_labels[$ti]}"

        log_row "" "%s\n" "$SEP"
        log "  ${BOLD}[ ${label} ]${NC}"
        log_row "" "$ROW_FMT" "SITE" "TOOL" "STATUS" "IP" "TIME(ms)" "VERDICT"
        log_row "" "%s\n" "$SEP"

        for site in "${SITES[@]}"; do
            TOTAL_QUERIES=$((TOTAL_QUERIES + 1))

            case "$tool" in
                dig)      result=$(query_dig      "$srv_ip"   "$srv_port" "$site") ;;
                nslookup) result=$(query_nslookup "$srv_ip"               "$site") ;;
                dig_doh)  result=$(query_dig_doh  "$srv_addr"             "$site") ;;
                curl_doh) result=$(query_curl_doh "$srv_addr"             "$site") ;;
            esac

            IFS='|' read -r status ip ms <<< "$result"

            if [[ "$status" == "resolved" ]]; then
                verdict="VIOLATION"
                color="$RED"
                SRV_VIOLATION=$((SRV_VIOLATION + 1))
                TOTAL_VIOLATION=$((TOTAL_VIOLATION + 1))
            else
                verdict="COMPLIANT"
                color="$GREEN"
                SRV_COMPLIANT=$((SRV_COMPLIANT + 1))
                TOTAL_COMPLIANT=$((TOTAL_COMPLIANT + 1))
            fi

            log_row "$color" "$ROW_FMT" \
                "$site" "$label" "$status" "${ip:--}" "${ms}ms" "$verdict"

            echo "${srv_name},${site},${label},${status},${ip:-},${ms},${verdict}" \
                >> "$CSV_FILE"
        done
    done

    SRV_ELAPSED=$(( $(now_ms) - SRV_START ))
    SRV_QUERIES=$(( NUM_SITES * ${#tool_keys[@]} ))

    log_row "" "%s\n" "$SEP"
    log "  ${BOLD}${srv_name} subtotal${NC}:  ${RED}${SRV_VIOLATION} violations${NC}  |  ${GREEN}${SRV_COMPLIANT} compliant${NC}  |  ${SRV_QUERIES} queries in ${SRV_ELAPSED}ms"
done

# ── Final summary ─────────────────────────────────────────────────────────────

GLOBAL_END=$(now_ms)
ELAPSED_MS=$(( GLOBAL_END - GLOBAL_START ))
ELAPSED_S=$(echo "scale=3; $ELAPSED_MS / 1000" | bc)
AVG_MS=$(echo "scale=1; $ELAPSED_MS / $TOTAL_QUERIES" | bc)

log ""
log "========================================================"
log "${BOLD}BENCHMARK COMPLETE${NC}"
log "========================================================"
log "Queries run   : ${BOLD}${TOTAL_QUERIES}${NC}  (sequential)"
log "Violations    : ${RED}${BOLD}${TOTAL_VIOLATION}${NC}  (sites resolving — ISP non-compliant)"
log "Compliant     : ${GREEN}${BOLD}${TOTAL_COMPLIANT}${NC}  (sites blocked — ISP compliant)"
log "--------------------------------------------------------"
log "Total time    : ${BOLD}${ELAPSED_S}s${NC}  (${ELAPSED_MS}ms)"
log "Avg per query : ${BOLD}${AVG_MS}ms${NC}"
log "--------------------------------------------------------"
log "Report (text) : ${REPORT_FILE}"
log "Report (CSV)  : ${CSV_FILE}"
log ""
log "${YELLOW}Tip: run 'go run ./cmd/crawler/ --sites site-list.txt --dns-servers dns-server.yaml'${NC}"
log "${YELLOW}     and compare total times to quantify the concurrency speedup.${NC}"
