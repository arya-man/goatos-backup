/* Import / export / templates (CSV + XLSX via SheetJS, loaded on demand) */
(function(){
  const IO={};
  const WORKBOOK_ORDER=['farms','parks','pens','partitions','species','breeds','sexes','stages','tags','healthStates','statusDefs','exitReasons','purposes','movementReasons','weightBands','rationGroups','animals','categories','items','roles','people','approvers','vendors','trucks','diseases','symptoms','deathCauses','marketCities','sopCategories','taskTypes','settings'];
  IO.ORDER=WORKBOOK_ORDER;
  const ALIASES={
    rfid:['rfid','rfid tag','tag','tag id','eid','animal identifier 1','animal id','rfid 1','primary tag'],
    tag2:['second tag','tag 2','rfid 2','animal identifier 2','old tag','vendor tag'],
    species:['species','animal type','type','goat or sheep'],
    breed:['breed'],sex:['sex','gender'],stage:['stage','lifecycle stage','shed tag','stage tag','management stage','pen tag'],
    dob:['dob','date of birth','birth date','born'],weight:['weight','weight kg','weight (kg)','kg'],
    park:['park','park name','farm park'],pen:['pen','pen name','shed','shed name','location'],partition:['partition','partition label','part'],
    tags:['tags','groups','tags & groups'],farm:['farm','farm code'],capacity:['capacity','cap','max animals'],
    name:['name'],code:['code'],fromD:['from','from days','from (days)','min age days'],toD:['to','to days','to (days)','max age days'],
    dept:['department','dept','module'],value:['value'],unit:['unit','uom'],category:['category','category path'],depts:['departments','used by'],
    role:['role','designation'],parks:['parks','park scope'],phone:['phone','mobile'],supplies:['supplies','vendor type','record type'],
    vendor:['vendor','transporter'],number:['vehicle number','truck','truck number','vehicle'],step:['approval step','step','what'],
    kind:['kind','tag or group'],group:['group','age group'],grade:['grade'],city:['city'],path:['category path','path','category']
  };
  const nk=s=>String(s||'').toLowerCase().replace(/[^a-z0-9]+/g,' ').trim();

  IO.matchHeaders=function(headers,regKey){
    const reg=REG.R[regKey]; const used=new Set();
    return headers.map(h=>{
      const n=nk(h); if(!n)return null;
      let c=reg.cols.find(c=>!used.has(c.k)&&(nk(c.label)===n||nk(c.k)===n));
      if(!c)c=reg.cols.find(c=>!used.has(c.k)&&(ALIASES[c.k]||[]).some(a=>nk(a)===n));
      if(c){used.add(c.k);return c.k;}
      return null;
    });
  };

  /* CSV */
  IO.parseCSV=function(text){
    const rows=[];let row=[],cell='',q=false;
    const delim=(text.split('\n')[0].split('\t').length>text.split('\n')[0].split(',').length)?'\t':',';
    for(let i=0;i<text.length;i++){
      const ch=text[i];
      if(q){if(ch==='"'){if(text[i+1]==='"'){cell+='"';i++;}else q=false;}else cell+=ch;}
      else if(ch==='"')q=true;
      else if(ch===delim){row.push(cell);cell='';}
      else if(ch==='\n'){row.push(cell);rows.push(row);row=[];cell='';}
      else if(ch!=='\r')cell+=ch;
    }
    if(cell!==''||row.length){row.push(cell);rows.push(row);}
    return rows.filter(r=>r.some(c=>String(c).trim()!==''));
  };
  const csvCell=v=>{v=String(v==null?'':v);return /[",\n]/.test(v)?'"'+v.replace(/"/g,'""')+'"':v;};
  IO.toCSV=rows=>rows.map(r=>r.map(csvCell).join(',')).join('\n')+'\n';

  /* SheetJS on demand */
  IO.xlsx=function(){
    if(window.XLSX)return Promise.resolve(window.XLSX);
    if(IO._xl)return IO._xl;
    IO._xl=new Promise((res,rej)=>{const s=document.createElement('script');s.src='https://cdn.jsdelivr.net/npm/xlsx@0.18.5/dist/xlsx.full.min.js';
      s.onload=()=>res(window.XLSX);s.onerror=()=>{IO._xl=null;rej(new Error('xlsx'));};document.head.appendChild(s);});
    return IO._xl;
  };

  function header(regKey){return REG.R[regKey].cols.map(c=>c.label);}
  function sampleRows(regKey,ids){
    const reg=REG.R[regKey];
    let list=ids&&ids.length?ids.map(id=>S.get(reg.coll,id)).filter(Boolean):S.active(reg.coll);
    return list.map(r=>{const v=REG.toRow(regKey,r);return reg.cols.map(c=>v[c.k]);});
  }
  function listsSheet(keys){
    const cols={};
    keys.forEach(k=>REG.R[k].cols.forEach(c=>{
      const title=c.label;
      if(cols[title])return;
      if(c.type==='enum'||c.type==='multienum')cols[title]=c.opts.filter(Boolean);
      else if(c.type==='ref'||c.type==='multi')cols[title]=[...new Set(S.active(c.ref).map(x=>REG.label(c.ref,x)))];
      else if(c.type==='path')cols[title]=S.active('categories').map(x=>REG.catPath(x.id));
    }));
    const titles=Object.keys(cols); const max=Math.max(0,...titles.map(t=>cols[t].length));
    const aoa=[titles]; for(let i=0;i<max;i++)aoa.push(titles.map(t=>cols[t][i]||''));
    return aoa;
  }
  const stamp=()=>{const d=new Date(),p=n=>String(n).padStart(2,'0');return d.getFullYear()+p(d.getMonth()+1)+p(d.getDate());};
  const fname=(k,kind,ext)=>'mesha-'+(k==='workbook'?'farm-setup':REG.R[k].label.toLowerCase().replace(/[^a-z0-9]+/g,'-'))+'-'+kind+'-'+stamp()+'.'+ext;

  async function writeBook(sheets,name){
    try{
      const X=await IO.xlsx(); const wb=X.utils.book_new();
      sheets.forEach(([title,aoa])=>{const ws=X.utils.aoa_to_sheet(aoa);ws['!cols']=(aoa[0]||[]).map(h=>({wch:Math.max(12,String(h).length+4)}));X.utils.book_append_sheet(wb,ws,title.slice(0,31));});
      const out=X.write(wb,{bookType:'xlsx',type:'array'});
      UI.download(name,new Blob([out],{type:'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet'}));
    }catch(e){
      UI.toast('Excel unavailable offline · downloading CSV');
      const [title,aoa]=sheets[0]; UI.download(name.replace(/\.xlsx$/,'.csv'),IO.toCSV(aoa));
    }
  }

  IO.template=function(regKey,fmt){
    if(regKey==='workbook')return IO.workbookTemplate(fmt);
    const aoa=[header(regKey)].concat(sampleRows(regKey).slice(0,2));
    if(fmt==='csv')return UI.download(fname(regKey,'template','csv'),IO.toCSV(aoa));
    writeBook([[REG.R[regKey].label,aoa],['Lists',listsSheet([regKey])]],fname(regKey,'template','xlsx'));
  };
  IO.exportRegister=function(regKey,fmt,ids){
    const aoa=[header(regKey).concat(['Status'])].concat(
      (ids&&ids.length?ids.map(id=>S.get(REG.R[regKey].coll,id)).filter(Boolean):S.all(REG.R[regKey].coll)).map(r=>{const v=REG.toRow(regKey,r);return REG.R[regKey].cols.map(c=>v[c.k]).concat([r.status]);}));
    if(fmt==='csv')return UI.download(fname(regKey,'export','csv'),IO.toCSV(aoa));
    writeBook([[REG.R[regKey].label,aoa]],fname(regKey,'export','xlsx'));
  };
  IO.workbookTemplate=function(fmt,withData){
    const sheets=WORKBOOK_ORDER.map(k=>[REG.R[k].label,[header(k)].concat(withData?sampleRows(k):sampleRows(k).slice(0,2))]);
    sheets.push(['Lists',listsSheet(WORKBOOK_ORDER)]);
    writeBook(sheets,fname('workbook',withData?'export':'template','xlsx'));
  };
  IO.downloadErrors=function(G){
    const reg=REG.R[G.regKey]; const res=Sheet.results(G);
    const aoa=[header(G.regKey).concat(['Errors'])];
    G.rows.forEach((r,i)=>{if(res[i].status!=='error')return;aoa.push(reg.cols.map(c=>r.v[c.k]||'').concat([Object.entries(res[i].issues).filter(([,x])=>x[0]==='err').map(([k,x])=>(reg.cols.find(c=>c.k===k)||{label:k}).label+': '+x[1]).join(' · ')]));});
    UI.download(fname(G.regKey,'errors','csv'),IO.toCSV(aoa));
  };

  /* ---------- import wizard state ---------- */
  IO.state=null;
  IO.start=function(regKey){
    IO.state={step:1,target:regKey||'animals',file:null,sheets:[],tab:0,summary:null};
    location.hash='#/configuration/items/import';
    App.render();
  };
  IO.sheetRegFor=function(name,fallback){
    const n=nk(name);
    const k=Object.keys(REG.R).find(k=>nk(REG.R[k].label)===n||nk(k)===n||nk(REG.R[k].one)===n||nk(REG.R[k].label).replace(/ & /,' ')===n);
    return k||(n==='lists'?null:fallback);
  };
  IO.readFile=async function(file){
    const st=IO.state; st.file=file.name;
    const ext=file.name.split('.').pop().toLowerCase();
    let raw=[];
    if(ext==='csv'||ext==='tsv'||ext==='txt'){raw=[{name:file.name.replace(/\.[^.]+$/,''),aoa:IO.parseCSV(await file.text())}];}
    else{
      try{const X=await IO.xlsx();const wb=X.read(await file.arrayBuffer(),{type:'array',cellDates:true});
        raw=wb.SheetNames.map(n=>({name:n,aoa:X.utils.sheet_to_json(wb.Sheets[n],{header:1,raw:false,defval:''})}));}
      catch(e){UI.toast('Could not read Excel file offline · use CSV');return;}
    }
    IO.load(raw);
  };
  IO.load=function(raw){
    const st=IO.state;
    st.sheets=raw.map(s=>{
      const aoa=(s.aoa||[]).filter(r=>r.some(c=>String(c).trim()!==''));
      const regKey=st.target==='workbook'?IO.sheetRegFor(s.name,null):(raw.length>1?IO.sheetRegFor(s.name,null)||st.target:st.target);
      const headers=(aoa[0]||[]).map(String);
      return {name:s.name,regKey,headers,data:aoa.slice(1),map:regKey?IO.matchHeaders(headers,regKey):[]};
    }).filter(s=>s.regKey&&s.data.length);
    if(st.target!=='workbook'&&st.sheets.length>1){const own=st.sheets.filter(s=>s.regKey===st.target);if(own.length)st.sheets=own;}
    if(!st.sheets.length){UI.toast('No rows found');return;}
    st.sheets.sort((a,b)=>WORKBOOK_ORDER.indexOf(a.regKey)-WORKBOOK_ORDER.indexOf(b.regKey));
    st.step=2; st.tab=0; App.render();
  };
  IO.buildGrids=function(){
    const st=IO.state;
    st.sheets.forEach((s,i)=>{
      const rows=s.data.map(r=>{const v={};s.map.forEach((k,j)=>{if(k)v[k]=r[j]==null?'':String(r[j]).trim();});return v;});
      s.G=Sheet.make(s.regKey,rows,{mode:'import'});
      s.G.pending=()=>IO.pendingBefore(i);
    });
    st.step=3; st.tab=0;
  };
  IO.pendingBefore=function(idx){
    const st=IO.state,p={};
    st.sheets.slice(0,idx).forEach(s=>{
      if(!s.G)return; const reg=REG.R[s.regKey];
      const set=p[reg.coll]||(p[reg.coll]=new Set());
      const main=reg.cols.find(c=>c.k==='name')||reg.cols.find(c=>c.k==='number')||reg.cols.find(c=>c.req);
      s.G.rows.forEach(r=>{if(r.v[main.k])set.add(REG.norm(r.v[main.k]));if(r.v.code)set.add(REG.norm(r.v.code));if(r.v.path)set.add(REG.norm(r.v.path));});
    });
    return p;
  };
  IO.commitAll=function(){
    const st=IO.state; st.snapN=S.snap('Import');
    st.lastRows=st.sheets.map(s=>JSON.parse(JSON.stringify(s.G.rows)));
    st.summary=st.sheets.map(s=>({label:REG.R[s.regKey].label,sum:REG.commit(s.regKey,s.G.rows,{createParents:s.G.createParents,pending:IO.pendingBefore(st.sheets.indexOf(s))})}));
    S.save(); st.step=4; App.render();
  };

  /* ---------- page ---------- */
  IO.page=function(){
    const st=IO.state||(IO.state={step:1,target:'animals',sheets:[]});
    const steps=['Upload','Match columns','Review','Done'];
    const head=`<div class="steps">${steps.map((s,i)=>`${i?'<i></i>':''}<span class="${st.step===i+1?'on':st.step>i+1?'done':''}"><b>${st.step>i+1?'✓':i+1}</b>${s}</span>`).join('')}</div>`;
    let body='';
    if(st.step===1){
      const opts=[['workbook','Farm setup workbook']].concat(WORKBOOK_ORDER.map(k=>[k,REG.R[k].label]));
      body=`<section class="card"><div class="hd"><h3>Upload</h3><span class="sp"></span>
          <select class="fsel" data-imp-target>${opts.map(([k,l])=>`<option value="${k}" ${st.target===k?'selected':''}>${esc(l)}</option>`).join('')}</select>
          <button class="btn sm" data-a="imp-tpl" data-f="xlsx">${ic('download')}Template .xlsx</button>
          ${st.target==='workbook'?`<button class="btn sm" data-a="imp-export-wb">${ic('download')}Export workbook</button>`:`<button class="btn sm" data-a="imp-tpl" data-f="csv">${ic('download')}Template .csv</button>`}
        </div><div class="bd stack">
          <label class="drop" data-drop>${ic('upload')}<div style="font-weight:700;margin-top:8px">Drop .xlsx or .csv</div>
            <input type="file" accept=".xlsx,.xls,.csv,.tsv,.txt" data-imp-file hidden></label>
          ${st.target!=='workbook'?`<div class="row"><button class="btn" data-a="imp-paste">${ic('sheet')}Paste from spreadsheet</button></div>`:''}
        </div></section>`;
    }
    if(st.step===2){
      body=st.sheets.map((s,si)=>{
        const reg=REG.R[s.regKey];
        const missing=reg.cols.filter(c=>c.req&&!s.map.includes(c.k));
        return `<section class="card mb"><div class="hd"><h3>${esc(s.name)}</h3><span class="cnt">${s.data.length} rows</span>
          <select class="fsel" data-imp-sheetreg="${si}">${WORKBOOK_ORDER.map(k=>`<option value="${k}" ${s.regKey===k?'selected':''}>${esc(REG.R[k].label)}</option>`).join('')}</select>
          <span class="sp"></span>${missing.length?UI.tag('Missing: '+missing.map(c=>c.label).join(', '),'warn'):UI.tag('All required matched','ok')}</div>
          <div class="twrap screen maptbl"><table><thead><tr><th>File column</th><th>Sample</th><th>Field</th></tr></thead><tbody>
          ${s.headers.map((h,j)=>`<tr><td><b>${esc(h)}</b></td><td class="muted">${esc((s.data.find(r=>String(r[j]||'').trim())||[])[j]||'')}</td>
            <td><select class="fsel" data-imp-map="${si}" data-j="${j}"><option value="">Ignore</option>${reg.cols.map(c=>`<option value="${c.k}" ${s.map[j]===c.k?'selected':''}>${esc(c.label)}${c.req?' *':''}</option>`).join('')}</select></td></tr>`).join('')}
          </tbody></table></div></section>`;
      }).join('')+`<div class="row"><button class="btn" data-a="imp-back">Back</button><span class="sp"></span><button class="btn p" data-a="imp-review">Review rows</button></div>`;
    }
    if(st.step===3){
      const s=st.sheets[st.tab];
      const totals=st.sheets.map((x,i)=>{const r=REG.validate(x.regKey,x.G.rows,{createParents:x.G.createParents,pending:IO.pendingBefore(i)});
        const newVals=new Set();r.forEach(y=>(y.creates||[]).forEach(c=>newVals.add(c.toLowerCase())));
        return {label:REG.R[x.regKey].label,create:r.filter(y=>y.status==='create').length,update:r.filter(y=>y.status==='update').length,same:r.filter(y=>y.status==='unchanged').length,err:r.filter(y=>y.status==='error').length,newv:newVals.size};});
      const sum=k=>totals.reduce((a,b)=>a+b[k],0);
      const saveN=sum('create')+sum('update'), errN=sum('err');
      body=`<section class="card mb"><div class="hd"><h3>Dry run</h3><span class="sp"></span><button class="btn sm" data-a="imp-report">${ic('download')}Report .csv</button></div>
        <div class="twrap screen"><table class="ltbl"><thead><tr><th>Sheet</th><th class="num">New</th><th class="num">Updates</th><th class="num">Unchanged</th><th class="num">Errors</th><th class="num">New values</th></tr></thead><tbody>
        ${totals.map((t,i)=>`<tr class="clk" data-a="imp-tab" data-i="${i}"><td><b>${esc(t.label)}</b></td><td class="num">${t.create?UI.tag(t.create,'ok'):0}</td><td class="num">${t.update?UI.tag(t.update,'info'):0}</td><td class="num">${t.same}</td><td class="num">${t.err?UI.tag(t.err,'dng'):0}</td><td class="num">${t.newv?UI.tag(t.newv,'pur'):0}</td></tr>`).join('')}
        </tbody></table></div></section>
        ${st.sheets.length>1?`<div class="tabs">${st.sheets.map((x,i)=>`<a href="javascript:void 0" class="${i===st.tab?'on':''}" data-a="imp-tab" data-i="${i}">${esc(REG.R[x.regKey].label)} ${totals[i].err?UI.tag(totals[i].err,'dng'):''}</a>`).join('')}</div>`:''}
        <div data-impsheet>${Sheet.html(s.G,{noFoot:true})}</div>
        <div class="row mt"><button class="btn" data-a="imp-back">Back</button><span class="sp"></span>${errN?UI.tag(errN+' skipped','dng'):''}
          <button class="btn p" data-a="imp-commit" ${saveN?'':'disabled'}>${ic('check')}Import ${saveN} row${saveN===1?'':'s'}</button></div>`;
    }
    if(st.step===4){
      body=`<section class="card"><div class="hd"><h3>Imported</h3><span class="sp"></span><button class="btn sm" data-a="imp-undo">${ic('undo')}Undo import</button><button class="btn sm p" data-a="imp-again">${ic('upload')}Import another</button></div>
        <div class="twrap screen"><table><thead><tr><th>Sheet</th><th>Created</th><th>Updated</th><th>Unchanged</th><th>Skipped</th><th>Parents created</th></tr></thead><tbody>
        ${st.summary.map(x=>`<tr><td><b>${esc(x.label)}</b></td><td>${UI.tag(x.sum.created,'ok')}</td><td>${UI.tag(x.sum.updated,'info')}</td><td>${x.sum.unchanged}</td><td>${x.sum.skipped?UI.tag(x.sum.skipped,'dng'):0}</td><td>${x.sum.parents?UI.tag(x.sum.parents,'pur'):0}</td></tr>`).join('')}
        </tbody></table></div></section>`;
    }
    return `<div class="phead"><div><div class="crumb">Configuration / <a href="#/configuration/items">Items and settings</a> / <b>Import</b></div><h1>Import</h1></div>
      <div class="sp"></div>${st.file?UI.tag(st.file,'mut'):''}</div>${head}${body}`;
  };
  IO.mount=function(root){
    const st=IO.state; if(!st)return;
    const f=root.querySelector('[data-imp-file]'); if(f)f.onchange=()=>{if(f.files[0])IO.readFile(f.files[0]);};
    const d=root.querySelector('[data-drop]');
    if(d){d.ondragover=e=>{e.preventDefault();d.classList.add('over');};d.ondragleave=()=>d.classList.remove('over');d.ondrop=e=>{e.preventDefault();d.classList.remove('over');if(e.dataTransfer.files[0])IO.readFile(e.dataTransfer.files[0]);};}
    const t=root.querySelector('[data-imp-target]'); if(t)t.onchange=()=>{st.target=t.value;App.render();};
    root.querySelectorAll('[data-imp-map]').forEach(s=>s.onchange=()=>{const sh=st.sheets[+s.dataset.impMap];sh.map[+s.dataset.j]=s.value||null;App.render();});
    root.querySelectorAll('[data-imp-sheetreg]').forEach(s=>s.onchange=()=>{const sh=st.sheets[+s.dataset.impSheetreg];sh.regKey=s.value;sh.map=IO.matchHeaders(sh.headers,sh.regKey);App.render();});
    const ish=root.querySelector('[data-impsheet]');
    if(ish){Sheet.activeGetter=()=>st.sheets[st.tab].G;Sheet.bind(ish,()=>st.sheets[st.tab].G,()=>App.render());}
  };
  Object.assign(A,{
    'imp-tpl'(el){IO.template(IO.state.target,el.dataset.f);},
    'imp-export-wb'(){IO.workbookTemplate('xlsx',true);},
    'imp-paste'(){Sheet.openEntry(IO.state.target,null,[{}]);},
    'imp-back'(){const st=IO.state;st.step=Math.max(1,st.step-1);App.render();},
    'imp-review'(){IO.buildGrids();App.render();},
    'imp-tab'(el){IO.state.tab=+el.dataset.i;App.render();},
    'imp-commit'(){IO.commitAll();},
    'imp-report'(){const st=IO.state;const aoa=[['Sheet','Row','Result','Issues']];
      st.sheets.forEach((x,i)=>{const r=REG.validate(x.regKey,x.G.rows,{createParents:x.G.createParents,pending:IO.pendingBefore(i)});const reg=REG.R[x.regKey];
        r.forEach((y,j)=>{if(y.status==='empty')return;aoa.push([reg.label,j+2,{create:'New',update:'Update',unchanged:'Unchanged',error:'Error'}[y.status],Object.entries(y.issues).map(([k,v])=>(reg.cols.find(c=>c.k===k)||{label:k}).label+': '+v[1]).join(' · ')]);});});
      UI.download('mesha-import-dry-run.csv',IO.toCSV(aoa));},
    'imp-undo'(){const st=IO.state;S.undoTo(st.snapN);st.sheets.forEach((s,i)=>{if(st.lastRows)s.G.rows=st.lastRows[i];});st.step=3;App.render();UI.toast('Import undone');},
    'imp-again'(){IO.start(IO.state.target);}
  });
  window.IO=IO;
})();
