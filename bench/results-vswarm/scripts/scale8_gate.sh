#!/usr/bin/env bash
# scale8_gate.sh OUT — one ghz (same invocation as sweep2.sh) at c=16 for 30 s against aesgo-mine from zero pods,
# with `kubectl get pods` logged every second (and podsamp.py). Read-only towards the cluster.
set -u
OUT=${1:?}; mkdir -p $OUT; V=$HOME/fc-vm/vswarm; PY=$HOME/fc-vm/tools/venv/bin/python; GHZ=$HOME/fc-vm/tools/ghz
source $V/fn.sh; fn_params aesgo
IP=$(kubectl -n istio-system get svc istio-ingressgateway -o jsonpath='{.spec.clusterIP}')
u=$(kubectl get ksvc aesgo-mine -o jsonpath='{.status.url}'); H=${u#*://}
echo "$(date '+%F %T') gate: pods now: $(kubectl get pods --no-headers 2>/dev/null | wc -l), firecracker: $(pgrep -x firecracker | wc -l), ingress $IP host $H"
( while :; do echo "=== $(date '+%F %T.%3N')"; kubectl get pods --no-headers 2>&1; sleep 1; done ) > $OUT/kubectl_pods_1s.log & KL=$!
$PY $V/podsamp.py aesgo-mine $OUT/gate & SP=$!
sleep 1.5
echo "$(date '+%F %T.%3N') ghz start"
/usr/bin/time -f '%U %S' -o $OUT/gate.time timeout -k 10 150 $GHZ --insecure --proto "$PROTO" --import-paths "$(dirname $PROTO)" \
  --call "$METHOD" -d "$DATA" --authority "$H" -c 16 --connections 16 -z 30s --timeout 60s --format json -o $OUT/gate.json "$IP:80" 2> $OUT/gate.err
echo "$(date '+%F %T.%3N') ghz exit=$?"
sleep 5; kill $KL $SP; wait $SP 2>/dev/null
kubectl get pods -o wide > $OUT/pods_after.txt 2>&1
python3 -c "
import json;j=json.load(open('$OUT/gate.json'));print('ok',j['statusCodeDistribution'],'rps',round(j['rps'],1),'total_s',j['total']/1e9)"
echo 'max pods / ready in samples:'; awk -F, 'NR>1{if(\$2>p)p=\$2; if(\$3>r)r=\$3; if(\$6>c)c=\$6} END{print p, r, \"max host busy cores\", c}' $OUT/gate.samples.csv
