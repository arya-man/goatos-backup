// Reuse the actual integrated app/model/compiler stack; add only DOM control fixtures.
const fs=require('fs');
const checks=String.raw`
vm.runInContext(fs.readFileSync(__dirname+'/questionnaire.js','utf8').split('\n').find(line=>line.startsWith('function questionnaireOptions(')),context);
run("current='Weighing';state.sops.Weighing=initialSop('Weighing');sop().nodes[1].answer='Multiple choice';sop().nodes[1].options=[{value:'a',label:'Option A'},{value:'aa',label:'Option AA'}];sop().nodes[2].ruleOwner='q1';sop().nodes[2].op='contains';sop().nodes[2].value='a';sop().nodes[1].ruleFallback=sop().nodes[2].no;");
function testControls(controls){context.document.querySelectorAll=()=>controls;run("qrTestRun('q1')");return element('#qr-test-result').textContent;}
const control=(value,checked)=>({dataset:{qrTest:'q1'},value,checked});
assert.match(run("qrTestField(sop().nodes[1])"),/type="checkbox"/);
assert.match(testControls([control('a',false),control('aa',true)]),/Otherwise/);
assert.equal(run("qrEvaluate(sop().nodes[2],{q1:['aa']})"),false);
assert.match(testControls([control('a',true),control('aa',true)]),/Matched/);
run("sop().nodes[2].op='not_contains'");assert.match(testControls([control('aa',true)]),/Matched/);
run("sop().nodes[2].op='unanswered'");assert.match(testControls([control('a',false),control('aa',false)]),/Matched/);
assert.match(testControls([control('removed',true)]),/available option/);
run("sop().nodes[1].answer='Single choice';sop().nodes[2].op='=';sop().nodes[2].value='aa'");assert.match(run("qrTestField(sop().nodes[1])"),/<select/);assert.match(testControls([control('aa')]),/Matched/);assert.match(testControls([control('Option AA')]),/available option/);
run("sop().nodes[1].answer='Yes / No';sop().nodes[2].value='Yes'");assert.match(testControls([control('Yes')]),/Matched/);assert.match(testControls([control('Maybe')]),/available option/);
run("sop().nodes[1].answer='Catalogue';sop().nodes[1].catalogueSourceId='source-judge-cat';current='Health';state.sops.Health=state.sops.Weighing;sop().nodes[2].value='judge-item'");assert.match(run("qrTestField(sop().nodes[1])"),/value="judge-item"/);assert.match(testControls([control('judge-item')]),/Matched/);assert.match(testControls([control('Judge item')]),/available option/);
console.log('PASS actual-stack branch tester: a/aa membership, NOT, blank, exact choice/catalogue IDs, stale values rejected');
`;
new Function('require','__dirname',fs.readFileSync(__dirname+'/judge-final-integration.cjs','utf8')+'\n'+checks)(require,__dirname);
