/* State store: localStorage, versioned key, snapshots for undo */
(function(){
  const KEY='mesha.config-sop-studio.v2.state@5';
  let seq=Date.now()%100000;
  const S={
    KEY, state:null, undoStack:[],
    uid(p){seq++;return (p||'id')+'_'+seq.toString(36)+Math.random().toString(36).slice(2,5);},
    load(){
      try{const raw=localStorage.getItem(KEY); if(raw){this.state=JSON.parse(raw);}}catch(e){this.state=null;}
      if(!this.state||!this.state.meta||this.state.meta.v!==3){this.state=window.buildSeed();}
      if(window.seedRefs&&!this.state.meta.refs){window.seedRefs(this.state);this.state.meta.refs=1;}
      this.save();
      return this.state;
    },
    save(){try{localStorage.setItem(KEY,JSON.stringify(this.state));}catch(e){}},
    reset(){try{localStorage.removeItem(KEY);}catch(e){} this.state=window.buildSeed(); if(window.seedRefs){window.seedRefs(this.state);this.state.meta.refs=1;} this.undoStack=[]; this.save();},
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
