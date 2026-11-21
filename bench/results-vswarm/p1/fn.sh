# fn.sh — per-function gRPC call parameters (sourced). fn_params <service-prefix> sets PROTO, METHOD, DATA.
P=$HOME/fc-vm/vswarm-src/vSwarm-proto/proto
fn_params() {
  case $1 in
    fibgo|fibpy) PROTO=$P/fibonacci/fibonacci.proto; METHOD=fibonacci.Greeter/SayHello; DATA='{"name":"10"}' ;;   # vSwarm relay default --value=10
    aesgo)       PROTO=$P/aes/aes.proto;             METHOD=aes.Aes/ShowEncryption;    DATA='{"plaintext_message":"world"}' ;;  # "world" -> server default plaintext
    authgo)      PROTO=$P/auth/auth.proto;           METHOD=auth.Greeter/SayHello;     DATA='{"name":"allow"}' ;;
    *) echo "unknown fn $1" >&2; return 1 ;;
  esac
}
