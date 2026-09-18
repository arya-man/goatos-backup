/* Import / export / templates (CSV + XLSX via SheetJS, loaded on demand) */
(function(){
  const IO={};
  const WORKBOOK_ORDER=['farms','parks','pens','partitions','species','breeds','sexes','stages','tags','healthStates','statusDefs','exitReasons','purposes','movementReasons','weightBands','animals','categories','items','lots','roles','people','designations','approvalChains','saleProducts','costKinds','identifierPolicies','sopCategories','taskTypes','salePrices','saleMinWeights','valuationRates','settings'].filter(k=>REG.R[k]);
  IO.ORDER=WORKBOOK_ORDER;
  IO.LIMITS={rows:10000,bytes:5*1024*1024,cols:60};
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

  /* per-species animal templates drop Breed/Stage when that species has none */
  function colsFor(regKey,speciesId){
    const reg=REG.R[regKey];
    /* items: kind fields of the open catalogue (all kinds when none is open) + capability columns */
    if(regKey==='items'){const kind=window.ItemsKind||'';return reg.cols.filter(c=>!c.kinds||!kind||c.kinds.includes(kind));}
    if(regKey!=='animals'||!speciesId)return reg.cols;
    return reg.cols.filter(c=>!(c.k==='breed'&&!S.active('breeds').some(x=>x.speciesId===speciesId))&&!(c.k==='stage'&&!S.active('stages').some(x=>x.speciesId===speciesId)));
  }
  function header(regKey,speciesId){return colsFor(regKey,speciesId).map(c=>c.label);}
  function sampleRows(regKey,ids,speciesId){
    const reg=REG.R[regKey],cols=colsFor(regKey,speciesId);
    let list=ids&&ids.length?ids.map(id=>S.get(reg.coll,id)).filter(Boolean):S.active(reg.coll);
    if(speciesId)list=list.filter(r=>r.speciesId===speciesId);
    return list.map(r=>{const v=REG.toRow(regKey,r);return cols.map(c=>v[c.k]);});
  }
  /* "Allowed values" sheet: human names only, scoped to the species when given */
  function listsSheet(keys,speciesId){
    const cols={};
    keys.forEach(k=>colsFor(k,speciesId).forEach(c=>{
      const title=c.label;
      if(cols[title])return;
      if(c.type==='enum'||c.type==='multienum')cols[title]=c.opts.filter(Boolean).map(REG.enumLabel);
      else if(c.type==='ref'||c.type==='multi'){
        let list=S.active(c.ref);
        if(speciesId&&c.ref==='species')list=list.filter(x=>x.id===speciesId);
        else if(speciesId&&c.scope&&c.scope.attr==='speciesId')list=list.filter(x=>x.speciesId===speciesId||(c.scope.allowBlank&&!x.speciesId));
        cols[title]=[...new Set(list.map(x=>REG.label(c.ref,x)))];
      }
      else if(c.type==='path')cols[title]=S.active('categories').map(x=>REG.catPath(x.id));
    }));
    const titles=Object.keys(cols); const max=Math.max(0,...titles.map(t=>cols[t].length));
    const aoa=[titles]; for(let i=0;i<max;i++)aoa.push(titles.map(t=>cols[t][i]||''));
    return aoa;
  }
  const stamp=()=>{const d=new Date(),p=n=>String(n).padStart(2,'0');return d.getFullYear()+p(d.getMonth()+1)+p(d.getDate());};
  const slug=t=>String(t).toLowerCase().replace(/[^a-z0-9]+/g,'-');
  const fname=(k,kind,ext,sp)=>'mesha-'+(k==='workbook'?'farm-setup':slug(REG.R[k].label))+(sp?'-'+slug(sp):'')+'-'+kind+'-'+stamp()+'.'+ext;
  /* formula-looking text is written as a literal so Excel never evaluates it */
  const safeCell=v=>typeof v==='string'&&/^[=+@]/.test(v)?"'"+v:v;

  async function writeBook(sheets,name,textCols){
    try{
      const X=await IO.xlsx(); const wb=X.utils.book_new();
      sheets.forEach(([title,aoa],si)=>{
        const ws=X.utils.aoa_to_sheet(aoa.map(r=>r.map(safeCell)));
        ws['!cols']=(aoa[0]||[]).map(h=>({wch:Math.max(12,String(h).length+4)}));
        const tc=(textCols&&textCols[si])||[];
        if(tc.length){ /* RFID columns as Text (@) so long tags never become 9.82E+14 */
          const rows=Math.max(aoa.length,501);
          tc.forEach(ci=>{for(let r=1;r<rows;r++){const ad=X.utils.encode_cell({r,c:ci});const cur=ws[ad];ws[ad]={t:'s',v:cur?String(cur.v):'',z:'@'};}});
          ws['!ref']=X.utils.encode_range({s:{r:0,c:0},e:{r:rows-1,c:Math.max((aoa[0]||[]).length-1,0)}});
        }
        X.utils.book_append_sheet(wb,ws,title.slice(0,31));
      });
      const out=X.write(wb,{bookType:'xlsx',type:'array'});
      UI.download(name,new Blob([out],{type:'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet'}));
    }catch(e){
      UI.toast('Excel unavailable offline · downloading CSV');
      const [title,aoa]=sheets[0]; UI.download(name.replace(/\.xlsx$/,'.csv'),IO.toCSV(aoa));
    }
  }
  const idCols=(regKey,speciesId)=>colsFor(regKey,speciesId).map((c,i)=>c.idTag?i:-1).filter(i=>i>=0);

  IO.template=function(regKey,fmt,speciesId){
    if(regKey==='workbook')return IO.workbookTemplate(fmt);
    const sp=speciesId&&S.get('species',speciesId);
    /* animal templates carry headers only (never real animals); allowed values live on their own sheet */
    const aoa=[header(regKey,speciesId)].concat(regKey==='animals'?[]:sampleRows(regKey,null,speciesId).slice(0,2));
    if(fmt==='csv')return UI.download(fname(regKey,'template','csv',sp&&sp.name),IO.toCSV(aoa));
    writeBook([[sp?sp.name+' '+REG.R[regKey].label.toLowerCase():REG.R[regKey].label,aoa],['Allowed values',listsSheet([regKey],speciesId)]],fname(regKey,'template','xlsx',sp&&sp.name),[idCols(regKey,speciesId)]);
  };
  IO.exportRegister=function(regKey,fmt,ids){
    const aoa=[header(regKey).concat(['Status'])].concat(
      (ids&&ids.length?ids.map(id=>S.get(REG.R[regKey].coll,id)).filter(Boolean):S.all(REG.R[regKey].coll).filter(r=>regKey!=='items'||!window.ItemsKind||REG.itemKindOfCat(r.categoryId)===window.ItemsKind)).map(r=>{const v=REG.toRow(regKey,r);return colsFor(regKey).map(c=>v[c.k]).concat([REG.enumLabel(r.status||'active')]);}));
    if(fmt==='csv')return UI.download(fname(regKey,'export','csv'),IO.toCSV(aoa.map(r=>r.map(safeCell))));
    writeBook([[REG.R[regKey].label,aoa]],fname(regKey,'export','xlsx'),[idCols(regKey)]);
  };
  IO.workbookTemplate=function(fmt,withData){
    const sheets=WORKBOOK_ORDER.map(k=>[REG.R[k].label,[header(k)].concat(withData?sampleRows(k):k==='animals'?[]:sampleRows(k).slice(0,2))]);
    sheets.push(['Allowed values',listsSheet(WORKBOOK_ORDER)]);
    writeBook(sheets,fname('workbook',withData?'export':'template','xlsx'),WORKBOOK_ORDER.map(k=>idCols(k)));
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
    App._allow=true;
    location.hash='#/configuration/items/import';
    App.render();
  };
  /* sheet name -> register: exact label/key first, then the earliest word that names a register ("Goat animals" -> Animals, "Pens & partitions" -> Pens) */
  const SHEET_WORDS={staff:'people',team:'people',employees:'people',approvals:'approvalChains',approvers:'approvalChains',sheds:'pens',shed:'pens',goats:'animals',sheep:'animals',herd:'animals',stock:'lots',receipts:'lots',receipt:'lots',lots:'lots',
    tags:'tags',stages:'stages',lifecycle:'stages',catalogue:'items',catalog:'items',inventory:'lots',titles:'designations',jobs:'designations',rules:'settings'};
  IO.sheetRegFor=function(name,fallback){
    const n=nk(name); if(!n)return fallback;
    if(n==='lists'||n==='allowed values')return null;
    if(REG.R.lots&&/^(inventory|stock|stock in|stock receipts?|receipts?|lots?|goods received)$/.test(n))return 'lots';
    const keys=Object.keys(REG.R);
    const exact=keys.find(k=>nk(REG.R[k].label)===n||nk(k)===n||nk(REG.R[k].one)===n||nk(REG.R[k].coll)===n);
    if(exact)return exact;
    const sing=w=>w.replace(/ies$/,'y').replace(/s$/,'');
    const words=n.split(' ').filter(w=>w.length>1&&w!=='and');
    for(const w of words){
      const hit=keys.find(k=>{const lw=nk(REG.R[k].label).split(' ').concat(nk(REG.R[k].one).split(' '));return nk(REG.R[k].label).split(' ').length===1&&lw.some(x=>sing(x)===sing(w));})
        ||keys.find(k=>nk(REG.R[k].label).split(' ').concat(nk(REG.R[k].one).split(' ')).some(x=>x.length>3&&sing(x)===sing(w)))
        ||(SHEET_WORDS[w]&&REG.R[SHEET_WORDS[w]]?SHEET_WORDS[w]:null);
      if(hit)return hit;
    }
    return fallback;
  };
  IO.reject=function(msg){const st=IO.state;st.error=msg;st.step=1;App.render();UI.toast(msg);};
  IO.readFile=async function(file){
    const st=IO.state; st.file=file.name; st.error=null;
    const ext=file.name.split('.').pop().toLowerCase();
    if(file.size>IO.LIMITS.bytes)return IO.reject('File is larger than 5 MB · split it into smaller files');
    let raw=[];
    if(ext==='csv'||ext==='tsv'||ext==='txt'){raw=[{name:file.name.replace(/\.[^.]+$/,''),aoa:IO.parseCSV(await file.text())}];}
    else{
      let wb,X;
      try{X=await IO.xlsx();wb=X.read(await file.arrayBuffer(),{type:'array',cellDates:true,cellFormula:true});}
      catch(e){UI.toast('Could not read Excel file offline · use CSV');return;}
      /* formula cells come in as their text and are flagged per cell in review */
      for(const n of wb.SheetNames){const ws=wb.Sheets[n];
        Object.keys(ws).forEach(k=>{const x=ws[k];if(k[0]!=='!'&&x&&x.f){const t='='+x.f;ws[k]={t:'s',v:t,w:t};}});}
      raw=wb.SheetNames.filter(n=>n!=='Allowed values'&&n!=='Lists').map(n=>({name:n,aoa:X.utils.sheet_to_json(wb.Sheets[n],{header:1,raw:false,defval:''})}));
    }
    IO.load(raw);
  };
  IO.load=function(raw){
    const st=IO.state;
    const big=raw.find(s=>(s.aoa||[]).length-1>IO.LIMITS.rows);
    if(big)return IO.reject('“'+big.name+'” has more than '+IO.LIMITS.rows.toLocaleString()+' rows · split the file');
    const wide=raw.find(s=>(s.aoa||[]).some(r=>r.length>IO.LIMITS.cols&&r.slice(IO.LIMITS.cols).some(c=>String(c).trim())));
    if(wide)return IO.reject('“'+wide.name+'” has more than '+IO.LIMITS.cols+' columns');
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
      s.G=Sheet.make(s.regKey,rows,{mode:'import',createParents:false});
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
    const st=IO.state;
    const t=IO.totals();
    if(t.err&&!st.skipErrors){UI.toast('Fix or skip '+t.err+' rows with errors first');return;}
    st.snapN=S.snap('Import');
    st.lastRows=st.sheets.map(s=>JSON.parse(JSON.stringify(s.G.rows)));
    st.summary=st.sheets.map(s=>({label:REG.R[s.regKey].label,sum:REG.commit(s.regKey,s.G.rows,{createParents:s.G.createParents,pending:IO.pendingBefore(st.sheets.indexOf(s))})}));
    S.save(); st.step=4; App.render();
  };
  IO.totals=function(){
    const st=IO.state;
    const rows=st.sheets.map((x,i)=>{const r=REG.validate(x.regKey,x.G.rows,{createParents:x.G.createParents,pending:IO.pendingBefore(i)});const c=Sheet.counts(r);
      return {label:REG.R[x.regKey].label,create:c.create,update:c.update,same:c.unchanged,err:c.error,newv:c.newv,unk:c.unresolved};});
    const sum=k=>rows.reduce((a,b)=>a+b[k],0);
    return {rows,create:sum('create'),update:sum('update'),err:sum('err'),newv:sum('newv'),unk:sum('unk')};
  };
  /* preview totals: a column that is zero on every sheet says nothing, so it is hidden (New stays) */
  const TOT_COLS=[['create','New'],['update','Updates'],['same','Unchanged'],['err','Errors'],['newv','New values']];
  const totVal=(x,k)=>k==='newv'?x.newv+x.unk:x[k];
  const totHide=(rows,k)=>k!=='create'&&!rows.some(x=>totVal(x,k));
  /* live refresh of the dry-run totals while cells are fixed (no rebuild, keeps focus) */
  IO.refreshTotals=function(root){
    const st=IO.state; if(!st||st.step!==3)return; const t=IO.totals();
    const cell=(n,tone)=>n?UI.tag(n,tone):'0';
    t.rows.forEach((x,i)=>{const tr=root.querySelector(`[data-tot="${i}"]`);if(!tr)return;
      tr.querySelector('[data-k="create"]').innerHTML=cell(x.create,'ok');tr.querySelector('[data-k="update"]').innerHTML=cell(x.update,'info');
      tr.querySelector('[data-k="same"]').textContent=x.same;tr.querySelector('[data-k="err"]').innerHTML=cell(x.err,'dng');tr.querySelector('[data-k="newv"]').innerHTML=cell(x.newv+x.unk,x.unk?'warn':'pur');});
    TOT_COLS.forEach(([k])=>{const d=totHide(t.rows,k)?'none':'';root.querySelectorAll(`[data-totk="${k}"],[data-tot] [data-k="${k}"]`).forEach(e=>e.style.display=d);});
    const saveN=t.create+t.update, ok=saveN&&(!t.err||st.skipErrors);
    const b=root.querySelector('[data-a="imp-commit"]'); if(b){b.disabled=!ok;b.innerHTML=IO.commitLabel(t,st);}
    const sk=root.querySelector('[data-impskip]'); if(sk){sk.style.display=t.err?'':'none';const n=sk.querySelector('[data-skiptxt]');if(n)n.textContent=Sheet.skipText(t.err);}
  };

  IO.commitLabel=(t,st)=>{const n=t.create+t.update;return ic('check')+(t.err&&!st.skipErrors?'Fix '+t.err+' row'+(t.err===1?'':'s')+' to import':n?'Import '+n+' row'+(n===1?'':'s'):'No changes');};
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
          ${st.error?`<div class="note dng" role="alert" style="color:var(--danger);font-weight:650">${ic('alert-triangle','',14)} ${esc(st.error)}</div>`:''}
          ${st.target==='animals'?`<div class="row" style="flex-wrap:wrap;gap:6px"><span class="muted small">Template</span>${S.active('species').map(sp=>`<button class="btn sm" data-a="imp-tpl" data-f="xlsx" data-sp="${sp.id}">${ic('download')}${esc(sp.name)} .xlsx</button>`).join('')}</div>`:''}
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
      const t=IO.totals(); const totals=t.rows;
      const saveN=t.create+t.update, errN=t.err;
      const cell=(n,tone)=>n?UI.tag(n,tone):0;
      body=`<section class="card mb"><div class="hd"><h3>Preview</h3></div>
        <div class="twrap screen"><table class="ltbl"><thead><tr><th>Sheet</th>${TOT_COLS.map(([k,l])=>`<th class="num" data-totk="${k}" style="${totHide(totals,k)?'display:none':''}">${l}</th>`).join('')}</tr></thead><tbody>
        ${totals.map((x,i)=>`<tr class="clk" data-a="imp-tab" data-i="${i}" data-tot="${i}"><td data-label="Sheet"><b>${esc(x.label)}</b></td><td class="num" data-label="New" data-k="create" style="${totHide(totals,'create')?'display:none':''}">${cell(x.create,'ok')}</td><td class="num" data-label="Updates" data-k="update" style="${totHide(totals,'update')?'display:none':''}">${cell(x.update,'info')}</td><td class="num" data-label="Unchanged" data-k="same" style="${totHide(totals,'same')?'display:none':''}">${x.same}</td><td class="num" data-label="Errors" data-k="err" style="${totHide(totals,'err')?'display:none':''}">${cell(x.err,'dng')}</td><td class="num" data-label="New values" data-k="newv" style="${totHide(totals,'newv')?'display:none':''}">${cell(x.newv+x.unk,x.unk?'warn':'pur')}</td></tr>`).join('')}
        </tbody></table></div></section>
        ${st.sheets.length>1?`<div class="tabs">${st.sheets.map((x,i)=>`<a href="javascript:void 0" class="${i===st.tab?'on':''}" data-a="imp-tab" data-i="${i}">${esc(REG.R[x.regKey].label)} ${totals[i].err?UI.tag(totals[i].err,'dng'):''}</a>`).join('')}</div>`:''}
        <div data-impsheet>${Sheet.html(s.G,{noFoot:true})}</div>
        <div class="row mt"><button class="btn" data-a="imp-back">Back</button><span class="sp"></span>
          <label class="row small" data-impskip style="font-weight:650;${errN?'':'display:none'}"><input type="checkbox" data-a="imp-skip" ${st.skipErrors?'checked':''}><span data-skiptxt>${Sheet.skipText(errN)}</span></label>
          <button class="btn p" data-a="imp-commit" ${saveN&&(!errN||st.skipErrors)?'':'disabled'}>${IO.commitLabel(t,st)}</button></div>`;
    }
    if(st.step===4){
      body=`<section class="card"><div class="hd"><h3>Imported</h3><span class="sp"></span><button class="btn sm" data-a="imp-undo">${ic('undo')}Undo import</button><button class="btn sm p" data-a="imp-again">${ic('upload')}Import another</button></div>
        <div class="twrap screen"><table><thead><tr><th>Sheet</th><th>Created</th><th>Updated</th><th>Unchanged</th><th>Skipped</th><th>New values created</th></tr></thead><tbody>
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
    if(ish){Sheet.activeGetter=()=>st.sheets[st.tab].G;st.sheets.forEach(x=>{x.G.onChange=()=>IO.refreshTotals(root);x.G.skipErrors=!!st.skipErrors;});Sheet.bind(ish,()=>st.sheets[st.tab].G,()=>App.render());}
  };
  Object.assign(A,{
    'imp-tpl'(el){IO.template(IO.state.target,el.dataset.f,el.dataset.sp);},
    'imp-skip'(el){IO.state.skipErrors=el.checked;App.render();},
    'imp-export-wb'(){IO.workbookTemplate('xlsx',true);},
    'imp-paste'(){Sheet.openEntry(IO.state.target,null,[{}]);},
    'imp-back'(){const st=IO.state;st.step=Math.max(1,st.step-1);App.render();},
    'imp-review'(){IO.buildGrids();App.render();},
    'imp-tab'(el){IO.state.tab=+el.dataset.i;App.render();},
    'imp-commit'(){IO.commitAll();},
    'imp-report'(){const st=IO.state;const aoa=[['Sheet','Row','Result','Issues']];
      st.sheets.forEach((x,i)=>{const r=REG.validate(x.regKey,x.G.rows,{createParents:x.G.createParents,pending:IO.pendingBefore(i)});const reg=REG.R[x.regKey];
        r.forEach((y,j)=>{if(y.status!=='error')return;aoa.push([reg.label,j+2,{create:'New',update:'Update',unchanged:'Unchanged',error:'Error'}[y.status],Object.entries(y.issues).map(([k,v])=>(reg.cols.find(c=>c.k===k)||{label:k}).label+': '+v[1]).join(' · ')].map(safeCell));});});
      UI.download('mesha-import-errors.csv',IO.toCSV(aoa));},
    'imp-undo'(){const st=IO.state;S.undoTo(st.snapN);st.sheets.forEach((s,i)=>{if(st.lastRows)s.G.rows=st.lastRows[i];});st.step=3;App.render();UI.toast('Import undone');},
    'imp-again'(){IO.start(IO.state.target);}
  });
  window.IO=IO;
})();
