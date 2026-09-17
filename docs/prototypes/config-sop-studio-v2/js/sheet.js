/* Spreadsheet grid: entry, bulk edit and import review share this component */
(function(){
  let seq=0;
  const rid=()=>'r'+(++seq);
  const Sheet={cur:null};

  Sheet.make=function(regKey,rowVals,opts){
    opts=opts||{};
    return {regKey,rows:rowVals.map(v=>({id:rid(),v:Object.assign({},v.v||v),recId:v.recId||null})),sel:new Set(),view:'all',
      createParents:!!opts.createParents,skipErrors:false,active:{r:0,c:0},pending:opts.pending||null,summary:null,title:opts.title,mode:opts.mode||'entry'};
  };

  /* open an entry grid page for a register */
  Sheet.openEntry=function(regKey,ids,vals){
    const reg=REG.R[regKey];
    let rows=[];
    if(ids&&ids.length)rows=ids.map(id=>{const r=S.get(reg.coll,id);return r?{v:REG.toRow(regKey,r),recId:id}:null;}).filter(Boolean);
    else rows=(vals&&vals.length?vals:[{}]);
    Sheet.cur=Sheet.make(regKey,rows,{title:ids&&ids.length?'Edit '+reg.label.toLowerCase():'Add '+reg.label.toLowerCase()});
    Sheet.cur.back=location.hash;
    App._allow=true;
    location.hash='#/configuration/items/sheet/'+regKey;
    App.render();
  };

  Sheet.results=function(G){
    const pend=G.pending?G.pending():{};
    return REG.validate(G.regKey,G.rows,{createParents:G.createParents,pending:pend});
  };
  Sheet.counts=function(res){
    const cnt={all:0,error:0,warn:0,info:0,create:0,update:0,unchanged:0,newv:0,unresolved:0};const nv=new Set();
    res.forEach(r=>{if(r.status==='empty')return;cnt.all++;cnt[r.status]++;const lv=Object.values(r.issues).map(i=>i[0]);if(lv.includes('warn'))cnt.warn++;if(lv.includes('info'))cnt.info++;
      (r.creates||[]).forEach(c=>nv.add(c.toLowerCase()));cnt.unresolved+=(r.unresolved||[]).length;});
    cnt.newv=nv.size; return cnt;
  };
  /* commit is blocked while any row has an error, unless the person explicitly chose to skip those rows */
  Sheet.canSave=(G,cnt)=>cnt.create+cnt.update>0&&(!cnt.error||G.skipErrors);
  Sheet.saveLabel=(G,cnt)=>{const n=cnt.create+cnt.update;return cnt.error&&!G.skipErrors?'Fix '+cnt.error+' row'+(cnt.error===1?'':'s')+' to save':n?'Save '+n+' row'+(n===1?'':'s'):'No changes';};
  Sheet.skipText=n=>'Skip '+n+' row'+(n===1?'':'s')+' with errors';
  const ISSUE_W=280, RN_W=60;
  Sheet.issuesHtml=function(G,ri,r){
    const reg=REG.R[G.regKey];
    const ents=Object.entries(r.issues).sort((a,b)=>({err:0,warn:1,info:2}[a[1][0]])-({err:0,warn:1,info:2}[b[1][0]]));
    if(!ents.length)return r.status==='empty'?'':`<span class="muted">${ic('check','',12)} OK</span>`;
    return ents.map(([k,i])=>{const ci=reg.cols.findIndex(x=>x.k===k),c=reg.cols[ci];const tone=i[0]==='err'?'dng':i[0]==='warn'?'warn':'info';
      const x=i[2];
      return `<div class="iss" style="display:flex;flex-wrap:wrap;align-items:center;gap:4px;padding:3px 0;white-space:normal">${UI.tag(i[0]==='err'?'Error':i[0]==='warn'?'Warning':'Info',tone)}<span>${esc((c?c.label+': ':'')+i[1])}</span>
        ${x&&x.create?`<button class="btn sm" data-a="sh-pick" data-r="${ri}" data-c="${ci}" data-t="${esc(x.text)}">Pick existing</button><button class="btn sm gh" data-a="sh-create" data-r="${ri}" data-c="${ci}" data-t="${esc(x.text)}">${ic('plus','',12)}${x.archived?'Restore':'Create'} “${esc(x.text)}”</button>`:''}</div>`;}).join('');
  };

  const HIDE_ZERO=['warn','info','create','update'];
  Sheet.chipHidden=(G,k,cnt)=>HIDE_ZERO.includes(k)&&!cnt[k]&&G.view!==k;
  Sheet.parentsShown=(G,cnt)=>!!(cnt.unresolved||cnt.newv||G.createParents);
  const STAT={create:['New','ok'],update:['Update','info'],unchanged:['Unchanged','mut'],error:['Error','dng'],empty:['','mut']};

  Sheet.html=function(G,opts){
    opts=opts||{};
    const reg=REG.R[G.regKey]; const res=Sheet.results(G); G._res=res;
    const cnt=Sheet.counts(res);
    const chips=[['all','All'],['error','Errors'],['warn','Warnings'],['info','Info'],['create','New'],['update','Updates'],['unchanged','Unchanged']];
    const stick=(left,w)=>`position:sticky;left:${left}px;min-width:${w}px;max-width:${w}px;width:${w}px;background:var(--panel-2);background-clip:padding-box;`;
    return `<div class="card screen" data-sheet>
      ${G.summary?Sheet.summaryHtml(G.summary):''}
      <div class="sheetbar">
        ${G.regKey==='animals'?`<label class="scanin">${ic('scan')}<input data-scan placeholder="Scan or type RFID" autocomplete="off"></label>`:''}
        <button class="btn sm" data-a="sh-addrow">${ic('plus')}Row</button>
        <button class="btn sm" data-a="sh-fill">${ic('arrow-down')}Fill down</button>
        <button class="btn sm" data-a="sh-delrows" ${G.sel.size?'':'disabled'}>${ic('trash')}Delete rows</button>
        <span class="sp"></span>
        <div class="chips">${chips.map(([k,l])=>`<button class="chip ${G.view===k?'on':''}" data-a="sh-view" data-v="${k}" style="${Sheet.chipHidden(G,k,cnt)?'display:none':''}">${l}&nbsp;<span data-cnt="${k}">${cnt[k]}</span></button>`).join('')}</div>
      </div>
      <div class="sheetbar">
        <label class="row small" data-parentswrap style="font-weight:650;${Sheet.parentsShown(G,cnt)?'':'display:none'}"><input type="checkbox" data-a="sh-parents" ${G.createParents?'checked':''}>Create all new values <span class="muted" data-cnt="unresolved">${cnt.unresolved?'('+cnt.unresolved+' unknown)':''}</span></label>
        ${opts.noFoot?'':`<label class="row small" style="font-weight:650;${cnt.error?'':'display:none'}" data-skipwrap><input type="checkbox" data-a="sh-skip" ${G.skipErrors?'checked':''}><span data-skiptxt>${Sheet.skipText(cnt.error)}</span></label>`}
        <span class="sp"></span>
        <span data-errnav class="row nw" style="gap:4px;${cnt.error?'':'display:none'}"><button class="btn sm" data-a="sh-err-prev" title="Previous error (Shift+F8 or ⌘↑)" aria-keyshortcuts="Shift+F8 Meta+ArrowUp"><span style="display:inline-flex;transform:rotate(180deg)">${ic('arrow-down')}</span>Previous error</button><button class="btn sm" data-a="sh-err-next" title="Next error (F8 or ⌘↓)" aria-keyshortcuts="F8 Meta+ArrowDown">${ic('arrow-down')}Next error</button></span>
        ${cnt.error?`<button class="btn sm" data-a="sh-errors">${ic('download')}Download errors</button>`:''}
        ${opts.noFoot?'':`<button class="btn sm" data-a="sh-cancel">Close</button>
        <button class="btn sm p" data-a="sh-save" ${Sheet.canSave(G,cnt)?'':'disabled'}>${ic('check')}${Sheet.saveLabel(G,cnt)}</button>`}
      </div>
      <style>[data-sheet] .rnst{display:none}
        @media(max-width:760px){[data-sheet] .sheet td.msg,[data-sheet] .sheet th.iss{position:static!important;box-shadow:none!important;min-width:200px!important;max-width:200px!important}
        [data-sheet] .sheet th.rn,[data-sheet] .sheet td.rn{min-width:88px!important;max-width:88px!important;width:88px!important;box-shadow:4px 0 6px -4px rgba(0,0,0,.45)}
        [data-sheet] .sheet td.rn label{height:auto;min-height:34px;flex-wrap:wrap;gap:2px 6px;padding:3px 0}
        [data-sheet] .rnst{display:block;width:100%}[data-sheet] .rnst .tag{font-size:10.5px;padding:1px 6px}
        [data-sheet] .sheet th.stc,[data-sheet] .sheet td.st{display:none}
        [data-sheet] .sheet{-webkit-mask-image:linear-gradient(to right,#000 calc(100% - 28px),transparent);mask-image:linear-gradient(to right,#000 calc(100% - 28px),transparent)}}</style>
      <div class="sheet" data-grid style="min-height:0">
        <table><thead><tr><th class="rn" style="${stick(0,RN_W)}z-index:4"><input type="checkbox" data-a="sh-selall" aria-label="Select all"></th><th class="iss" style="${stick(RN_W,ISSUE_W)}z-index:4;border-right:1px solid var(--line);box-shadow:4px 0 6px -4px rgba(0,0,0,.45)">Issues</th><th class="stc">Status</th>
          ${reg.cols.map(c=>`<th class="${c.req?'req':''}">${esc(c.label)}</th>`).join('')}</tr></thead>
        <tbody>${G.rows.map((row,ri)=>Sheet.rowHtml(G,row,ri,res[ri])).join('')}</tbody></table>
      </div></div>`;
  };

  Sheet.rowHtml=function(G,row,ri,r){
    const reg=REG.R[G.regKey]; const st=STAT[r.status]||STAT.empty;
    const hid=Sheet.hidden(G,r);
    return `<tr data-r="${ri}" class="${G.sel.has(row.id)?'selr':''} ${hid?'hid':''}">
      <td class="rn" style="left:0;z-index:3;min-width:${RN_W}px;max-width:${RN_W}px"><label><input type="checkbox" data-a="sh-sel" data-id="${row.id}" ${G.sel.has(row.id)?'checked':''}>${ri+1}<span class="rnst">${st[0]?UI.tag(st[0],st[1]):''}</span></label></td>
      <td class="msg" data-issues style="position:sticky;left:${RN_W}px;z-index:3;min-width:${ISSUE_W}px;max-width:${ISSUE_W}px;white-space:normal;padding:2px 8px;background:var(--panel);background-clip:padding-box;border-right:1px solid var(--line);box-shadow:4px 0 6px -4px rgba(0,0,0,.45)">${Sheet.issuesHtml(G,ri,r)}</td>
      <td class="st">${st[0]?UI.tag(st[0],st[1]):''}</td>
      ${reg.cols.map((c,ci)=>{const iss=r.issues[c.k];return `<td class="w-${c.w||''} ${iss?(iss[0]==='err'?'err':iss[0]==='warn'?'warn':'info'):''}" title="${iss?esc(iss[1]):''}"><input class="cell" data-r="${ri}" data-c="${ci}" value="${esc(row.v[c.k]==null?'':row.v[c.k])}" autocomplete="off" aria-label="${esc(c.label)} row ${ri+1}"></td>`;}).join('')}
    </tr>`;
  };

  Sheet.hidden=function(G,r){
    if(G.view==='all')return false;
    if(r.status==='empty')return true;
    if(G.view==='warn'||G.view==='info')return !Object.values(r.issues).some(i=>i[0]===G.view);
    return r.status!==G.view;
  };

  Sheet.summaryHtml=function(s){
    return `<div class="sheetbar" style="background:var(--brand-soft)">
      <b>${ic('check','',15)} Saved</b><span class="tag t-ok">${s.created} created</span><span class="tag t-info">${s.updated} updated</span>
      <span class="tag t-mut">${s.unchanged} unchanged</span>${s.skipped?`<span class="tag t-dng">${s.skipped} skipped</span>`:''}${s.parents?`<span class="tag t-pur">${s.parents} new values created</span>`:''}
      <span class="sp"></span><button class="btn sm" data-a="sh-undo">${ic('undo')}Undo</button></div>`;
  };

  /* repaint validation without rebuilding inputs (keeps focus) */
  Sheet.repaint=function(G,root){
    const res=Sheet.results(G); G._res=res; const reg=REG.R[G.regKey];
    root.querySelectorAll('tbody tr[data-r]').forEach(tr=>{
      const ri=+tr.dataset.r, r=res[ri]; if(!r)return; const st=STAT[r.status]||STAT.empty;
      tr.querySelector('td.st').innerHTML=st[0]?UI.tag(st[0],st[1]):'';const rs=tr.querySelector('.rnst');if(rs)rs.innerHTML=st[0]?UI.tag(st[0],st[1]):'';
      reg.cols.forEach((c,ci)=>{const inp=tr.querySelector(`input.cell[data-c="${ci}"]`);if(!inp)return;const td=inp.parentElement,iss=r.issues[c.k];
        td.classList.toggle('err',!!iss&&iss[0]==='err');td.classList.toggle('warn',!!iss&&iss[0]==='warn');td.classList.toggle('info',!!iss&&iss[0]==='info');td.title=iss?iss[1]:'';});
      tr.querySelector('[data-issues]').innerHTML=Sheet.issuesHtml(G,ri,r);
    });
    const cnt=Sheet.counts(res);
    root.querySelectorAll('[data-cnt]').forEach(el=>{const k=el.dataset.cnt;el.textContent=k==='unresolved'?(cnt.unresolved?'('+cnt.unresolved+' unknown)':''):cnt[k];
      const chip=el.closest('.chip');if(chip)chip.style.display=Sheet.chipHidden(G,k,cnt)?'none':'';});
    const pw=root.querySelector('[data-parentswrap]'); if(pw)pw.style.display=Sheet.parentsShown(G,cnt)?'':'none';
    const en=root.querySelector('[data-errnav]'); if(en)en.style.display=cnt.error?'':'none';
    const sk=root.querySelector('[data-skipwrap]'); if(sk){sk.style.display=cnt.error?'':'none';const t=sk.querySelector('[data-skiptxt]');if(t)t.textContent=Sheet.skipText(cnt.error);}
    const sv=root.querySelector('[data-a="sh-save"]');
    if(sv){sv.disabled=!Sheet.canSave(G,cnt);sv.innerHTML=ic('check')+Sheet.saveLabel(G,cnt);}
    if(G.onChange)G.onChange(cnt,res);
  };

  /* changing a parent location clears children that do not belong to it (never re-creates them under the new parent) */
  Sheet.cascade=function(G,row,k){
    const reg=REG.R[G.regKey]; const cleared=[];
    const kids=reg.cols.filter(c=>c.scope&&c.scope.col===k&&(c.ref==='pens'||c.ref==='partitions'));
    kids.forEach(c=>{const raw=String(row.v[c.k]||'').trim();if(!raw)return;
      const f=REG.findRef(G.regKey,c,raw,row.v);if(!f.rec){row.v[c.k]='';cleared.push(c.k);cleared.push(...Sheet.cascade(G,row,c.k));}});
    return cleared;
  };
  const syncInputs=(root,G,ri,keys)=>{const reg=REG.R[G.regKey];keys.forEach(k=>{const ci=reg.cols.findIndex(c=>c.k===k);const el=root.querySelector(`input.cell[data-r="${ri}"][data-c="${ci}"]`);if(el)el.value=G.rows[ri].v[k]||'';});};

  /* binding: a sheet lives in a root element; G resolved through getter */
  Sheet.bind=function(root,getG,rerender){
    const G=()=>getG();
    let t;
    root.addEventListener('input',e=>{
      const inp=e.target.closest('input.cell'); if(!inp)return;
      const g=G(),reg=REG.R[g.regKey],c=reg.cols[+inp.dataset.c];
      g.rows[+inp.dataset.r].v[c.k]=inp.value;
      if(isPick(c))openPop(inp);
      clearTimeout(t); t=setTimeout(()=>Sheet.repaint(g,root),120);
    });
    root.addEventListener('focusin',e=>{
      const inp=e.target.closest('input.cell'); if(!inp)return;
      const g=G(); g.active={r:+inp.dataset.r,c:+inp.dataset.c}; Sheet._lastCell={r:inp.dataset.r,c:inp.dataset.c};
      const c=REG.R[g.regKey].cols[g.active.c]; if(isPick(c))openPop(inp); else UI.closePop();
    });
    root.addEventListener('change',e=>{const inp=e.target.closest('input.cell');if(!inp)return;const g=G(),c=REG.R[g.regKey].cols[+inp.dataset.c],ri=+inp.dataset.r;
      const cl=Sheet.cascade(g,g.rows[ri],c.k);if(cl.length){syncInputs(root,g,ri,cl);Sheet.repaint(g,root);}});
    root.addEventListener('focusout',e=>{const inp=e.target.closest('input.cell');if(inp)setTimeout(()=>{if(UI._popCfg&&UI._popCfg.input===inp&&document.activeElement!==inp)UI.closePop();},150);});
    root.addEventListener('keydown',e=>{
      const inp=e.target.closest('input.cell');
      if(e.key==='F8'||(e.metaKey||e.ctrlKey)&&(e.key==='ArrowDown'||e.key==='ArrowUp'))return;
      if(inp){
        if(UI.popKey(e)){return;}
        const g=G();
        if(e.key==='Tab'&&g.view==='error'){e.preventDefault();Sheet.jumpError(e.shiftKey?-1:1,inp);return;}
        if(e.key==='Enter'&&!e.altKey&&inp.parentElement.classList.contains('err')){
          const ri=+inp.dataset.r,ci=+inp.dataset.c,c=REG.R[g.regKey].cols[ci],r=(g._res||[])[ri],iss=r&&r.issues[c.k];
          if(iss&&iss[2]&&iss[2].create){e.preventDefault();Sheet.pickPop(g,inp,ri,ci,iss[2].text);return;}
          if(isPick(c)){e.preventDefault();openPop(inp);return;}
        }
        if(e.key==='Enter'||e.key==='ArrowDown'&&e.altKey){e.preventDefault();move(inp,1,0);}
        if(e.key==='ArrowUp'&&e.altKey){e.preventDefault();move(inp,-1,0);}
      }
      const sc=e.target.closest('[data-scan]');
      if(sc&&e.key==='Enter'){e.preventDefault();const val=sc.value.trim();if(!val)return;Sheet.scanAppend(G(),val);sc.value='';rerender();setTimeout(()=>{const s=root.querySelector('[data-scan]');if(s)s.focus();},0);}
    });
    root.addEventListener('paste',e=>{
      const inp=e.target.closest('input.cell'); if(!inp)return;
      const text=(e.clipboardData||window.clipboardData).getData('text');
      if(!/[\t\n]/.test(text.replace(/\n$/,'')))return;
      e.preventDefault();
      Sheet.pasteAt(G(),+inp.dataset.r,+inp.dataset.c,text); rerender();
    });
    function move(inp,dr,dc){
      const g=G(); let r=+inp.dataset.r+dr; if(r>=g.rows.length){g.rows.push({id:rid(),v:{}});rerender();}
      setTimeout(()=>{const n=root.querySelector(`input.cell[data-r="${r}"][data-c="${+inp.dataset.c+dc}"]`);if(n){n.focus();n.select();}},0);
    }
    function isPick(c){return ['ref','path','enum','multi','multienum','steps'].includes(c.type);}
    function openPop(inp){
      const g=G(),regKey=g.regKey,reg=REG.R[regKey],c=reg.cols[+inp.dataset.c],row=g.rows[+inp.dataset.r];
      UI.pop(inp,UI.pickerCfg(regKey,c,row.v,inp,()=>{row.v[c.k]=inp.value;syncInputs(root,g,+inp.dataset.r,Sheet.cascade(g,row,c.k));Sheet.repaint(g,root);},null));
    }
  };

  /* error navigation: F8 / Shift+F8 or Cmd/Ctrl+Down / Up; wraps around; focuses the cell */
  Sheet.errorCells=()=>Array.from(document.querySelectorAll('[data-sheet] tbody tr[data-r]:not(.hid) td.err input.cell'));
  Sheet.jumpError=function(dir,from){
    const cells=Sheet.errorCells(); if(!cells.length){UI.toast('No errors');return;}
    let cur=from||document.activeElement;
    if(!(cur&&cur.matches&&cur.matches('input.cell'))&&Sheet._lastCell){const l=document.querySelector(`[data-sheet] input.cell[data-r="${Sheet._lastCell.r}"][data-c="${Sheet._lastCell.c}"]`);if(l)cur=l;}
    let i=cells.indexOf(cur);
    if(i<0){i=dir>0?cells.findIndex(c=>cur&&cur.compareDocumentPosition&&(cur.compareDocumentPosition(c)&Node.DOCUMENT_POSITION_FOLLOWING)):-1;
      if(dir>0){if(i<0)i=0;}else{const after=cells.findIndex(c=>cur&&cur.compareDocumentPosition&&(cur.compareDocumentPosition(c)&Node.DOCUMENT_POSITION_FOLLOWING));i=(after<0?cells.length:after)-1;if(i<0)i=cells.length-1;}}
    else i=(i+dir+cells.length)%cells.length;
    const el=cells[i]; UI.closePop(); el.focus(); el.select();
    try{el.scrollIntoView({block:'center',inline:'center'});}catch(e){}
  };
  document.addEventListener('keydown',e=>{
    if(!document.querySelector('[data-sheet]'))return;
    const nav=e.key==='F8'?(e.shiftKey?-1:1):(e.metaKey||e.ctrlKey)&&!e.altKey&&(e.key==='ArrowDown'||e.key==='ArrowUp')?(e.key==='ArrowDown'?1:-1):0;
    if(!nav)return;
    e.preventDefault(); Sheet.jumpError(nav);
  });

  /* Pick existing for an unknown value: filtered by that value, best match highlighted,
     applied to every row with the same value unless unticked */
  Sheet.pickPop=function(G,inp,ri,ci,text){
    const reg=REG.R[G.regKey],c=reg.cols[ci],row=G.rows[ri]; if(!c||!row)return;
    const multi=c.type==='multi'; text=String(text||row.v[c.k]||'').trim(); const nt=REG.norm(text);
    const toks=v=>String(v==null?'':v).split(/[;,]/).map(s=>s.trim()).filter(Boolean);
    const has=r=>multi?toks(r.v[c.k]).some(s=>REG.norm(s)===nt):REG.norm(r.v[c.k])===nt;
    const targets=G.rows.map((r,i)=>has(r)?i:-1).filter(i=>i>=0);
    const cfg={multi:true,fuzzy:true,applyAll:true,query:()=>text,
      header:()=>targets.length>1?`<label class="none" style="display:flex;gap:6px;align-items:center;cursor:pointer;border-bottom:1px solid var(--line2)"><input type="checkbox" data-popall ${cfg.applyAll?'checked':''}>Apply to all ${targets.length} rows with “${esc(text)}”</label>`:'',
      options:()=>UI.refOptions(G.regKey,c,row.v),
      creates:q=>REG.createChoices(G.regKey,multi?Object.assign({},c,{type:'ref'}):c,row.v,q),
      onPick(o){
        const idx=cfg.applyAll?targets:[ri];
        idx.forEach(i=>{const r=G.rows[i];
          r.v[c.k]=multi?toks(r.v[c.k]).map(s=>REG.norm(s)===nt?o.label:s).join('; '):o.label;
          Sheet.cascade(G,r,c.k);});
        setTimeout(()=>{Sheet.rerender();UI.toast((o.created?'Created':'Set')+' “'+o.label+'” · '+idx.length+' row'+(idx.length===1?'':'s'));},0);
      }};
    UI.pop(inp,cfg);
  };

  Sheet.pasteAt=function(G,r0,c0,text){
    const reg=REG.R[G.regKey];
    let lines=text.replace(/\r/g,'').replace(/\n$/,'').split('\n').map(l=>l.split('\t'));
    // header row detection: first line matches column labels
    const map=IO.matchHeaders(lines[0],G.regKey);
    const hits=map.filter(Boolean).length;
    if(hits>=2&&hits>=lines[0].length/2){
      lines.slice(1).forEach((cells,i)=>{
        const ri=r0+i; while(G.rows.length<=ri)G.rows.push({id:rid(),v:{}});
        cells.forEach((val,j)=>{if(map[j])G.rows[ri].v[map[j]]=val.trim();});
      });
      return;
    }
    lines.forEach((cells,i)=>{
      const ri=r0+i; while(G.rows.length<=ri)G.rows.push({id:rid(),v:{}});
      cells.forEach((val,j)=>{const c=reg.cols[c0+j];if(c)G.rows[ri].v[c.k]=val.trim();});
    });
  };

  Sheet.scanAppend=function(G,rfid){
    const last=[...G.rows].reverse().find(r=>!REG.isBlankRow(REG.R[G.regKey],r.v));
    const blank=G.rows.find(r=>REG.isBlankRow(REG.R[G.regKey],r.v));
    const v={rfid};
    if(last)['species','breed','sex','stage','park','pen','partition','tags'].forEach(k=>{if(last.v[k])v[k]=last.v[k];});
    if(blank)blank.v=v; else G.rows.push({id:rid(),v});
  };

  Sheet.fillDown=function(G){
    const reg=REG.R[G.regKey],c=reg.cols[G.active.c],src=G.rows[G.active.r]; if(!c||!src)return 0;
    const val=src.v[c.k]||''; let n=0;
    const targets=G.sel.size?G.rows.filter(r=>G.sel.has(r.id)&&r!==src):G.rows.slice(G.active.r+1);
    targets.forEach(r=>{r.v[c.k]=val;Sheet.cascade(G,r,c.k);n++;});
    return n;
  };

  Sheet.save=function(G){
    const reg=REG.R[G.regKey];
    const res=Sheet.results(G);
    G.lastRows=JSON.parse(JSON.stringify(G.rows));
    G.snapN=S.snap('Saved '+reg.label.toLowerCase());
    const sum=REG.commit(G.regKey,G.rows,{createParents:G.createParents,pending:G.pending?G.pending():{},strict:!G.skipErrors});
    if(sum.blocked){S.undoTo(G.snapN);UI.toast('Fix or skip rows with errors first');return sum;}
    S.save();
    // keep only rows that failed
    G.rows=G.rows.filter((r,i)=>res[i].status==='error');
    if(!G.rows.length)G.rows=[{id:rid(),v:{}}];
    G.sel.clear(); G.summary=sum; G.view='all';
    return sum;
  };

  /* page-level actions for the current entry sheet */
  const cur=()=>Sheet.cur;
  Object.assign(A,{
    'sh-addrow'(){const g=Sheet.active();g.rows.push({id:rid(),v:{}});Sheet.rerender();},
    'sh-view'(el){const g=Sheet.active();g.view=el.dataset.v;Sheet.rerender();},
    'sh-parents'(el){const g=Sheet.active();g.createParents=el.checked;Sheet.rerender();},
    'sh-skip'(el){const g=Sheet.active();g.skipErrors=el.checked;Sheet.rerender();},
    /* one-click resolve of an unknown value: create it (choosing a parent when needed) */
    'sh-create'(el){const g=Sheet.active(),reg=REG.R[g.regKey],c=reg.cols[+el.dataset.c],row=g.rows[+el.dataset.r];if(!c||!row)return;
      const cc=c.type==='multi'?Object.assign({},c,{type:'ref'}):c;
      const ch=REG.createChoices(g.regKey,cc,row.v,el.dataset.t);
      const doIt=x=>{const n=S.snap('Created '+el.dataset.t);x.run();S.save();Sheet.rerender();UI.toast('Created “'+el.dataset.t+'”',()=>{S.undoTo(n);Sheet.rerender();});};
      if(!ch.length)return UI.toast('Set '+(c.scope?reg.cols.find(x=>x.k===c.scope.col).label:'the value')+' first');
      if(ch.length===1)doIt(ch[0]);else UI.menu(el,ch.map(x=>({label:x.label,icon:'plus',run:()=>doIt(x)})));},
    'sh-pick'(el){const inp=document.querySelector(`[data-sheet] input.cell[data-r="${el.dataset.r}"][data-c="${el.dataset.c}"]`);if(!inp)return;
      inp.focus();inp.select();setTimeout(()=>Sheet.pickPop(Sheet.active(),inp,+el.dataset.r,+el.dataset.c,el.dataset.t),0);},
    'sh-sel'(el){const g=Sheet.active();el.checked?g.sel.add(el.dataset.id):g.sel.delete(el.dataset.id);Sheet.rerender();},
    'sh-selall'(el){const g=Sheet.active();g.rows.forEach(r=>el.checked?g.sel.add(r.id):g.sel.delete(r.id));Sheet.rerender();},
    'sh-delrows'(){const g=Sheet.active();g.rows=g.rows.filter(r=>!g.sel.has(r.id));if(!g.rows.length)g.rows=[{id:rid(),v:{}}];g.sel.clear();Sheet.rerender();},
    'sh-fill'(){const g=Sheet.active();const n=Sheet.fillDown(g);Sheet.rerender();UI.toast(n?'Filled '+n+' rows':'Focus a cell first');},
    'sh-err-next'(){Sheet.jumpError(1);},
    'sh-err-prev'(){Sheet.jumpError(-1);},
    'sh-errors'(){const g=Sheet.active();IO.downloadErrors(g);},
    'sh-cancel'(){const g=cur();App.leave(g&&g.back&&!g.back.includes('/sheet/')?g.back:'#/configuration/items/'+(g?g.regKey:''));},
    'sh-save'(){const g=Sheet.active();if(Sheet.saveHook)return Sheet.saveHook(g);Sheet.save(g);Sheet.rerender();},
    'sh-undo'(){const g=Sheet.active();S.undoTo(g.snapN);g.summary=null;if(g.lastRows)g.rows=g.lastRows;App.render();UI.toast('Undone');}
  });
  Sheet.active=()=>Sheet.activeGetter?Sheet.activeGetter():Sheet.cur;
  Sheet.rerender=()=>App.render();

  window.Sheet=Sheet;
})();
