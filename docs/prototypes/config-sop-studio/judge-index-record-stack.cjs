const fs=require('fs'),path=require('path'),assert=require('assert/strict');
const html=fs.readFileSync(path.join(__dirname,'index.html'),'utf8');
const scripts=[...html.matchAll(/<script\b[^>]*\bsrc="([^"]+)"/g)].map(m=>m[1].split('?')[0]);
for(const file of scripts)assert.equal(fs.existsSync(path.join(__dirname,file)),true,'Missing index script: '+file);
const tail=['farm-setup.js','record-workspace.js','record-category-manager.js','animal-setup.js','vendor/xlsx.full.min.js','animal-register.js','modal-accessibility.js'];
assert.deepEqual(scripts.slice(scripts.indexOf('farm-setup.js')),tail,'Review new tail overrides and expand integration/browser coverage when index order changes');
for(const file of tail)assert.equal(scripts.filter(s=>s===file).length,1);
const owners={};for(const file of scripts){const source=fs.readFileSync(path.join(__dirname,file),'utf8');for(const name of ['recordAdd','recordEdit','renderItems','recordManageCategories','bulkAnimalSetup'])if(new RegExp('(?:function\\s+'+name+'\\s*\\(|\\b'+name+'\\s*=\\s*function\\s*\\()').test(source))owners[name]=file;}
assert.equal(owners.recordAdd,'animal-register.js');assert.equal(owners.recordEdit,'animal-register.js');assert.equal(owners.renderItems,'animal-register.js');assert.equal(owners.recordManageCategories,'record-category-manager.js');assert.equal(owners.bulkAnimalSetup,'animal-setup.js');
console.log('PASS actual index assets, ordered record override tail and final handler ownership; DOM interactions require browser evidence');
