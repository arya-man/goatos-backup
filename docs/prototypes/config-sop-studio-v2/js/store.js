/* State store: localStorage, versioned key, snapshots for undo */
(function(){
  const KEY='mesha.config-sop-studio.v2.state@14';
  window.GLOBAL_SEXES=()=>[{id:'sx_female',code:'female',key:'female',name:'Female',aliases:['F'],status:'active'},{id:'sx_male',code:'male',key:'male',name:'Male',aliases:['M'],status:'active'}];
  let seq=Date.now()%100000;
  const S={
    KEY, state:null, undoStack:[],
    uid(p){seq++;return (p||'id')+'_'+seq.toString(36)+Math.random().toString(36).slice(2,5);},
    load(){
      try{const raw=localStorage.getItem(KEY); if(raw){this.state=JSON.parse(raw);}}catch(e){this.state=null;}
      if(!this.state||!this.state.meta||this.state.meta.v!==3){this.state=window.buildSeed();}
      if(window.seedRefs&&!this.state.meta.refs){window.seedRefs(this.state);this.state.meta.refs=1;}
      this.migrateSexes();this.tidy();this.save();
      return this.state;
    },
    /* sexes are one global register (female/male); collapse any per-species rows and remap references */
    migrateSexes(){const st=this.state,g=window.GLOBAL_SEXES(),map={};
      (st.sexes||[]).forEach(x=>{const n=String(x.code||x.name||'').trim().toLowerCase();map[x.id]=/^(f|female)$/.test(n)?'sx_female':/^(m|male)$/.test(n)?'sx_male':'';});
      (st.animals||[]).forEach(a=>{if(a.sexId&&map[a.sexId]!==undefined)a.sexId=map[a.sexId];});
      ['stages','pens'].forEach(c=>(st[c]||[]).forEach(r=>{if(r.sexId&&map[r.sexId]!==undefined){const to=map[r.sexId];r.sex=to?to.slice(3):(c==='pens'?'mixed':'any');delete r.sexId;}}));
      st.sexes=g;},
    /* placeholder publishers ("CEO / CXO 01") read as the role, not a person */
    tidy(){const fix=v=>(v||[]).forEach(x=>{if(x&&/\s0\d$/.test(x.by||''))x.by=x.by.replace(/\s0\d$/,'');});(this.state.sops||[]).forEach(r=>fix(r.versions));(this.state.masters||[]).forEach(r=>fix(r.versions));Object.values(this.state).forEach(c=>Array.isArray(c)&&c.forEach(r=>r&&r.versions&&fix(r.versions)));},
    save(){try{localStorage.setItem(KEY,JSON.stringify(this.state));}catch(e){}},
    reset(){try{localStorage.removeItem(KEY);}catch(e){} this.state=window.buildSeed(); if(window.seedRefs){window.seedRefs(this.state);this.state.meta.refs=1;} this.migrateSexes(); this.tidy(); this.undoStack=[]; this.save();},
    snap(label){this.undoStack.push({label,json:JSON.stringify(this.state)}); if(this.undoStack.length>30)this.undoStack.shift(); return this.undoStack.length;},
    undo(){const s=this.undoStack.pop(); if(!s)return false; this.state=JSON.parse(s.json); this.save(); return s.label;},
    undoTo(n){if(!n||n>this.undoStack.length)return false; const s=this.undoStack[n-1]; this.undoStack.length=n-1; this.state=JSON.parse(s.json); this.save(); return s.label;},
    all(c){return this.state[c]||(this.state[c]=[]);},
    active(c){return this.all(c).filter(r=>r.status!=='archived');},
    get(c,id){return id?this.all(c).find(r=>r.id===id):null;},
    add(c,rec){const r=Object.assign({id:this.uid(c.slice(0,3)),status:'active'},rec); this.all(c).push(r); return r;},
    update(c,id,patch){const r=this.get(c,id); if(r)Object.assign(r,patch); return r;},
    remove(c,ids){const set=new Set([].concat(ids)); this.state[c]=this.all(c).filter(r=>!set.has(r.id));}
  };
  window.S=S;
})();
