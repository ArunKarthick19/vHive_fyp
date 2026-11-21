#!/usr/bin/env python3
"""cold1.py IP HOST FN BODYFILE — one gRPC call (new channel), like curl in cold.sh. Prints "<status> <time_total_s>".
time_total: from just before the call (channel connect included, imports excluded) to the full reply. Reply message
text is written to BODYFILE (exact bytes compared between modes)."""
import os, sys, time
sys.path.insert(0, os.path.join(os.path.dirname(os.path.abspath(__file__)), 'pb'))
import grpc
ip, host, fn, bf = sys.argv[1:5]
if fn in ('fibgo', 'fibpy'):
    from fibonacci import fibonacci_pb2 as pb, fibonacci_pb2_grpc as pbg; stub, req, fld = pbg.GreeterStub, pb.HelloRequest(name='10'), 'SayHello'
elif fn == 'aesgo':
    from aes import aes_pb2 as pb, aes_pb2_grpc as pbg; stub, req, fld = pbg.AesStub, pb.PlainTextMessage(plaintext_message='world'), 'ShowEncryption'
elif fn == 'authgo':
    from auth import auth_pb2 as pb, auth_pb2_grpc as pbg; stub, req, fld = pbg.GreeterStub, pb.HelloRequest(name='allow'), 'SayHello'
t0 = time.monotonic()
ch = grpc.insecure_channel(f'{ip}:80', options=[('grpc.default_authority', host)])
try:
    r = getattr(stub(ch), fld)(req, timeout=300)
    t = time.monotonic() - t0
    open(bf, 'w').write(str(r))
    print('OK', f'{t:.6f}')
except grpc.RpcError as e:
    t = time.monotonic() - t0
    open(bf, 'w').write(f'{e.code().name}: {e.details()}')
    print(e.code().name, f'{t:.6f}')
