#!/usr/bin/env python3
"""Numeric A/B against immutable PR21; isolated namespaces, no published binaries."""
import argparse, hashlib, io, json, os, pathlib, shutil, statistics, subprocess, tarfile
from check_lowmem_lab import summarize
ROOT=pathlib.Path(__file__).resolve().parents[1]
BASE='f04bdbc101c8f9f86c2cc6a1cc512e9d2373504c'
def run(args,**kw):
 return subprocess.check_output([str(x) for x in args],text=True,**kw)
def main():
 p=argparse.ArgumentParser()
 for name in ['go','work','report-dir']:p.add_argument('--'+name,required=True)
 p.add_argument('--repetitions',type=int,default=3);a=p.parse_args()
 work=pathlib.Path(a.work).resolve();work.mkdir(mode=0o700,parents=True,exist_ok=False)
 out=pathlib.Path(a.report_dir).resolve();out.mkdir(parents=True,exist_ok=False)
 go=pathlib.Path(a.go).resolve()
 env={**os.environ,'GOENV':'off','GOTOOLCHAIN':'local','GOWORK':'off','GOTELEMETRY':'off','CGO_ENABLED':'0','GOOS':'linux','GOARCH':'amd64','GOMAXPROCS':'2'}
 env.pop('GOROOT',None)
 head=run(['git','-C',ROOT,'rev-parse','HEAD']).strip()
 assert not run(['git','-C',ROOT,'status','--porcelain']).strip(),'commit all measured sources first'
 baseline=work/'baseline'
 with tarfile.open(fileobj=io.BytesIO(subprocess.check_output(['git','-C',ROOT,'archive',BASE]))) as archive:archive.extractall(baseline,filter='data')
 test=(ROOT/'go_client/packet_pipe_pressure_test.go').read_text()
 test=test.replace('"encoding/json"','"encoding/json"\n"github.com/cbeuw/connutil"').replace('newBoundedPacketPipe()', 'connutil.AsyncPacketPipe()')
 (baseline/'go_client/packet_pipe_pressure_test.go').write_text(test)
 result={'baseline':BASE,'candidate':head,'toolchain':run([go,'version'],env=env).strip(),'scope':'native synthetic Linux; no VK or hardware acceptance','pressure':{},'sessions':{},'binary_sha256':{}}
 cpus=sorted(os.sched_getaffinity(0))[:2];prefix=['taskset','-c',','.join(map(str,cpus))];result['cpu_affinity']=cpus
 for variant,source in [('baseline',baseline),('candidate',ROOT)]:
  binary=work/(variant+'.test');run([go,'test','-mod=readonly','-trimpath','-buildvcs=false','-c','-o',binary],cwd=source/'go_client',env=env)
  result['binary_sha256'][variant]=hashlib.sha256(binary.read_bytes()).hexdigest()
 run([go,'test','-mod=readonly','-trimpath','-buildvcs=false','-c','-o',work/'server.test'],cwd=ROOT/'hardened-relay',env=env)
 for rep in range(1,a.repetitions+1):
  for workers in [9,18]:
   for variant in ['baseline','candidate']:
    stem=f'{variant}-{workers}-{rep}';pressure=out/(stem+'-pressure.json')
    run([*prefix,work/(variant+'.test'),'-test.run=^TestBoundedPipePressureLab$','-test.timeout=30s'],env={**env,'WDTT_PIPE_LAB_REPORT':str(pressure),'WDTT_PIPE_LAB_WORKERS':str(workers)})
    result['pressure'][stem]=json.loads(pressure.read_text())
    # Same harness/traffic for each immutable runtime, separate namespace/run.
    run(['unshare','-n','sh','-ec','ip link set lo up; exec "$@"','lab',*prefix,'python3',ROOT/'tools/check_lowmem_session.py','--harness',work/'server.test','--client-test',work/(variant+'.test'),'--workers',workers,'--report',out/(stem+'-report.json'),'--samples',out/(stem+'-samples.jsonl')],env=env)
    result['sessions'][stem]=summarize(out/(stem+'-samples.jsonl'))
    (out/'comparison.json').write_text(json.dumps(result,indent=2)+'\n')
 print('PASS immutable PR21 paired 9/18 pressure and TCP/UDP runs')
if __name__=='__main__':main()
