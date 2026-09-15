/* Compound answer rules: one decision, multiple typed checks. */
const ruleOperators={'=':'Equals','!=':'Does not equal','>':'Greater than','>=':'Greater than or equal','<':'Less than','<=':'Less than or equal','contains':'Includes','not_contains':'Does not include','between':'Between (inclusive)','answered':'Has an answer','unanswered':'Has no answer'};
const ruleCompareBase=compare;
compare=function(a,op,b){if(op==='answered')return a!==undefined&&a!==null&&a!==''&&(!Array.isArray(a)||a.length>0);if(op==='unanswered')return !compare(a,'answered','');if(op==='not_contains')return compare(a,'answered','')&&!ruleCompareBase(a,'contains',b);if(op==='between'){const bounds=String(b).split('..').map(Number);return compare(a,'answered','')&&bounds.length===2&&bounds.every(Number.isFinite)&&String(b).split('..').every(x=>x.trim())&&bounds[0]<=bounds[1]&&!Array.isArray(a)&&Number(a)>=bounds[0]&&Number(a)<=bounds[1]}return ruleCompareBase(a,op,b)};
const ruleInspectBase=inspect;
inspect=function(){ruleInspectBase();const n=sop().nodes.find(x=>x.id===selected),p=$('#inspector');if(!n||n.type!=='condition'||!p)return;const dis=canEdit()?'':'disabled',select=p.querySelector('[aria-label="Decision comparison"]');if(select)select.innerHTML=Object.entries(ruleOperators).map(([v,l])=>`<option value="${v}" ${v===n.op?'selected':''}>${esc(l)} (${esc(v)})</option>`).join('');p.insertAdjacentHTML('beforeend',`<section class="question-settings"><h3>Combine answer checks</h3><label class="field">Match<select aria-label="Rule matching" onchange="ruleSet('match',this.value)" ${dis}><option value="all" ${n.match!=='any'?'selected':''}>ALL checks (AND)</option><option value="any" ${n.match==='any'?'selected':''}>ANY check (OR)</option></select></label><p class="muted">The main check above and the checks below form one decision. Between uses lower..upper, for example 30..35.</p>${(n.clauses||[]).map((c,i)=>`<div class="card"><select aria-label="Check ${i+2} answer" onchange="ruleClause(${i},'source',this.value)" ${dis}>${sop().nodes.filter(q=>q.type==='question').map(q=>`<option value="${q.id}" ${q.id===c.source?'selected':''}>${esc(q.label)}</option>`).join('')}</select><select aria-label="Check ${i+2} comparison" onchange="ruleClause(${i},'op',this.value)" ${dis}>${Object.entries(ruleOperators).map(([v,l])=>`<option value="${v}" ${v===c.op?'selected':''}>${esc(l)}</option>`).join('')}</select><input aria-label="Check ${i+2} value" value="${esc(c.value||'')}" onchange="ruleClause(${i},'value',this.value)" ${dis}><button onclick="ruleRemove(${i})" ${dis}>Remove check</button></div>`).join('')}<button onclick="ruleAdd()" ${dis}>+ Add answer check</button></section>`)};
function ruleMutate(fn){if(!canEdit())return;const n=sop().nodes.find(x=>x.id===selected);if(n?.type!=='condition')return;const before=JSON.stringify(sop().nodes);fn(n);persist();canvasRemember(before);drawGraph();inspect()}
function ruleSet(k,v){ruleMutate(n=>n[k]=v)}
function ruleClause(i,k,v){ruleMutate(n=>n.clauses[i][k]=v)}
function ruleAdd(){ruleMutate(n=>(n.clauses??=[]).push({source:n.source,op:'=',value:''}))}
function ruleRemove(i){ruleMutate(n=>n.clauses.splice(i,1))}
const ruleSimBase=renderSim;
renderSim=function(){const n=sim?.nodes?.find(x=>x.id===sim.id);if(n?.type==='condition'&&n.clauses?.length){const checks=[n,...n.clauses],results=checks.map(c=>compare(sim.answers[c.source],c.op,c.value)),ok=n.match==='any'?results.some(Boolean):results.every(Boolean);sim.path.push(n.label+': '+(n.match==='any'?'ANY':'ALL')+' checks → '+(ok?'Match':'Otherwise'));sim.id=ok?n.yes:n.no;return renderSim()}return ruleSimBase()};
const ruleValidateBase=validateWorkflow;
function ruleQuestionPrecedes(nodes, questionId, decisionId){
 const map=new Map(nodes.map(n=>[n.id,n])),start=nodes.find(n=>n.type==='start'),seen=new Set();
 if(!start)return false;
 const stack=[start.id];
 while(stack.length){const id=stack.pop();if(id===questionId||seen.has(id))continue;if(id===decisionId)return false;seen.add(id);const n=map.get(id);if(n)for(const p of canvasPorts(n))if(n[p.key])stack.push(n[p.key]);}
 return true;
}
validateWorkflow=function(s,m=current){
 const normalized=structuredClone(s);
 for(const n of normalized.nodes)if(n.type==='condition'&&['not_contains','between','answered','unanswered'].includes(n.op)){
  const q=normalized.nodes.find(q=>q.id===n.source);n.op='=';
  n.value=q?.answer==='Number'?'0':q?.answer==='Catalogue'?(nodeCatalogue(q,m)[0]?.id||'validation-placeholder'):Array.isArray(q?.options)?q.options[0]?.value||'validation-placeholder':q?.answer==='Yes / No'?'Yes':(q?.choices||'validation-placeholder').split(',')[0].trim();
 }
 const errors=ruleValidateBase(normalized,m);
 for(const n of s.nodes.filter(x=>x.type==='condition')){
  if(n.match!==undefined&&!['all','any'].includes(n.match))errors.push(n.label+': matching must be ALL or ANY.');
  if(n.clauses!==undefined&&!Array.isArray(n.clauses)){errors.push(n.label+': additional checks must be a list.');continue;}
  for(const c of [n,...n.clauses||[]]){
   if(!c||typeof c!=='object'){errors.push(n.label+': invalid answer check.');continue;}
   const q=s.nodes.find(q=>q.id===c.source&&q.type==='question');
   if(!q){errors.push(n.label+': choose an existing answer for every check.');continue;}
   if(c!==n&&!ruleQuestionPrecedes(s.nodes,q.id,n.id))errors.push(n.label+': '+q.label+' must be collected on every path before this decision.');
   if(!Object.hasOwn(ruleOperators,c.op))errors.push(n.label+': unsupported comparison.');
   const value=String(c.value??'').trim();
   if(['>','>=','<','<=','between'].includes(c.op)&&q.answer!=='Number')errors.push(n.label+': numeric comparisons require a Number answer.');
   if(c.op==='between'){
    const parts=value.split('..');if(parts.length!==2||parts.some(x=>!x.trim()||!Number.isFinite(Number(x)))||Number(parts[0])>Number(parts[1]))errors.push(n.label+': range must be lower..upper.');
   }else if(!['answered','unanswered'].includes(c.op)&&!value)errors.push(n.label+': every comparison needs a value.');
   if(q.answer==='Number'&&!['answered','unanswered','between'].includes(c.op)&&(!Number.isFinite(Number(value))||!value||['contains','not_contains'].includes(c.op)))errors.push(n.label+': numeric answers need a numeric comparison.');
   if(['Single choice','Multiple choice','Catalogue','Yes / No'].includes(q.answer)&&!['answered','unanswered'].includes(c.op)){
    const values=q.answer==='Catalogue'?nodeCatalogue(q,m).map(i=>String(i.id)):q.answer==='Yes / No'?['Yes','No']:questionnaireOptions(q).map(o=>String(o.value));
    if(!values.includes(String(c.value)))errors.push(n.label+': comparison refers to an unavailable answer choice.');
   }
  }
 }
 return [...new Set(errors)];
};
const ruleBranchBase=canvasBranchLabel;canvasBranchLabel=function(n){return n.clauses?.length?`${n.match==='any'?'ANY':'ALL'} of ${n.clauses.length+1} checks`:ruleBranchBase(n)};
