import sys,json,dataclasses,os
from pathlib import Path
root=Path(os.environ.get('HEALTH_SOP_SOURCE', '../health-sop'))
if not root.is_dir():
 raise SystemExit('Set HEALTH_SOP_SOURCE to the separate health-sop checkout for this optional source comparison.')
sys.path.insert(0,str(root/'adult'))
from stories.mini_engine import evaluate
src=Path(__file__).with_name('health-engine-data.js').read_text()
d=json.loads(src[src.index('=')+1:].rstrip().rstrip(';'))
paths={'adult':'adult','kid_milk':'kids/milk-drinking','kid_weaning':'kids/weaning','kid_fattening':'kids/fattening'}
def norm(v):
 if isinstance(v,dict):return {k:norm(x) for k,x in v.items()}
 if isinstance(v,(list,tuple,set)):return sorted((norm(x) for x in v),key=lambda x:json.dumps(x,sort_keys=True))
 return v
count=0
for klass,path in paths.items():
 cat=json.loads((root/path/'stories/catalog.json').read_text())
 pack=d['packs'][klass]
 assert len(pack['stories'])==cat['count']
 for original,recorded in zip(cat['stories'],pack['stories']):
  for key in ['id','title','animal','findings','ctx','expect']:
   assert original.get(key,{})==recorded.get(key,{}),(klass,original['id'],key)
  actual=dataclasses.asdict(evaluate(original['animal'],original['findings'],**original.get('ctx',{})))
  assert norm(actual)==norm(recorded['recordedResult']),(klass,original['id'],'oracle output mismatch')
  count+=1
print(f'PASS {count} story inputs/expectations and full recorded oracle results match current source')
