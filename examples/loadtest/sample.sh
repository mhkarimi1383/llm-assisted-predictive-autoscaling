#!/usr/bin/env bash
# sample.sh <mode-name> <out-csv> — every 10 s record, for the load-test
# target: replica counts (spec + ready), aggregate pod CPU (milli-cores)
# and memory (MiB), CPU utilization vs requests, and HPA desired replicas
# when an HPA exists.
#
# kubectl top pods columns: NAME CPU(mem: cores|m) MEMORY(mem: Mi|Gi)
# CPU unit "m"=milli-cores; memory normalized to MiB.
set -u
MODE="$1"; OUT="$2"
echo "time,mode,spec_replicas,ready_replicas,cpu_milli,mem_mib,cpu_pct_of_requests,hpa_desired" > "$OUT"
REQ_MILLI=500
end=$((SECONDS + 960))
while [ $SECONDS -lt $end ]; do
  ts=$(date +%s)
  spec=$(kubectl -n default get deploy burn -o jsonpath='{.spec.replicas}' 2>/dev/null || echo "?")
  ready=$(kubectl -n default get deploy burn -o jsonpath='{.status.readyReplicas}' 2>/dev/null || echo "0")
  stats=$(kubectl -n default top pods -l app=burn --no-headers 2>/dev/null \
    | awk '{ c=$2; sub(/m$/,"",c); if ($2 !~ /m$/) c=$2*1000;
            m=$3; sub(/Mi$/,"",m); if ($3 ~ /Gi$/) m=$3*1024; sub(/Ki$/,"",m); if ($3 ~ /Ki$/) m=$3/1024;
            s+=c; sm+=m }
          END { printf "%.0f %.1f", s+0, sm+0 }')
  CPU=$(echo "$stats" | cut -d" " -f1)
  MEM=$(echo "$stats" | cut -d" " -f2)
  [ -z "$CPU" ] && CPU=0
  [ -z "$MEM" ] && MEM=0
  pct=$(awk -v c="$CPU" -v n="${spec:-2}" -v r="$REQ_MILLI" 'BEGIN { p=0; if (n>0) p=(c/(n*r))*100; printf "%.1f", p }')
  hpa=$(kubectl -n default get hpa burn-hpa -o jsonpath='{.status.desiredReplicas}' 2>/dev/null || echo "")
  echo "$ts,$MODE,$spec,${ready:-0},$CPU,$MEM,$pct,$hpa" >> "$OUT"
  sleep 10
done
