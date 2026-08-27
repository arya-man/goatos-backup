#!/usr/bin/env python3
"""Live persona sweep for per-person access (maintainer decision 2026-08-27).

Runs against a LOCAL stack and asks, for every active person on the roster and for
every screen they can see, whether the product actually works for them:

  PASS 1  every person, web + mobile bootstrap, and the invariants -- a module they
          do not hold is ABSENT (not greyed, not an empty heading), every visible
          leaf is a screen they are ticked for, and every leaf has a page contract
  PASS 2  grant / revoke per distinct role-shape, on ONE token minted before the
          first change, proving a change lands without a log out and log in
  PASS 3  the dead-screen sweep: open the DATA ROUTE behind every visible leaf and
          fail on a 403. This is what a static test cannot see, and it is what found
          nine dead leaves across four real people
  PASS 4  reflection timing, and what a phone tick does today
  PASS 5  the refusal matrix -- what the save must NOT accept

Not a unit test and not part of CI: it needs the local API, the local database and a
seeded roster. Run it after a change to the access model, before landing.

  make dev-local (or the manual stack), then:
  python3 tools/dev/audit-person-access.py
"""

import json, subprocess, urllib.request, urllib.error, os, sys

API="http://127.0.0.1:8080"
TENANT="00000000-0000-4000-8000-000000000001"
ENV={**os.environ,"GOATOS_AUTH_ISSUER":"goatos-local","GOATOS_AUTH_AUDIENCE":"goatos-api",
     "GOATOS_AUTH_HS256_SECRET":"goatos-local-dev-secret-32-bytes-min"}

def mint(uid):
    return subprocess.run(["/tmp/mintbin","-user-id",uid,"-tenant-id",TENANT,"-ttl","2h"],
                          capture_output=True,text=True,env=ENV).stdout.strip()

def call(path, tok, method="GET", body=None):
    req=urllib.request.Request(API+path, method=method)
    req.add_header("Authorization","Bearer "+tok)
    if body is not None:
        req.add_header("Content-Type","application/json")
        req.data=json.dumps(body).encode()
    try:
        with urllib.request.urlopen(req) as r:
            raw=r.read()
            return r.status, (json.loads(raw) if raw else None)
    except urllib.error.HTTPError as e:
        raw=e.read()
        try: return e.code, json.loads(raw)
        except Exception: return e.code, None

def psql(q):
    return subprocess.run(["psql","postgres://postgres:goatos@127.0.0.1:5433/goatos?sslmode=disable","-tAc",q],
                          capture_output=True,text=True).stdout.strip()

people=[]
for line in psql("""select m.display_name, coalesce(m.user_id::text,''), m.workforce_member_id::text,
  coalesce(string_agg(distinct g.role,'+' order by g.role),'(none)')
from workforce_members m
left join user_scope_grants g on g.user_id=m.user_id and g.tenant_id=m.tenant_id and g.status='active'
where m.status='active' group by 1,2,3 order by 4,1""").split("\n"):
    if not line.strip(): continue
    n,u,mid,roles=line.split("|")
    people.append({"name":n,"uid":u,"member":mid,"roles":roles})

P=[0];F=[0];NOTES=[]
def chk(ok,label,detail=""):
    if ok: P[0]+=1
    else:
        F[0]+=1
        NOTES.append(f"{label} :: {detail}")
    return ok

# ---- catalog: module -> its tickable pages (from the backend, one read) ----
CEO=[p for p in people if p["roles"]=="ceo_internal"][0]
ceo_tok=mint(CEO["uid"])
st,acc=call(f"/admin/workforce/people/{CEO['member']}/access", ceo_tok)
assert st==200, f"catalog read failed {st}"
PAGE_MODULE={}      # page_key -> module_key
MODULE_PAGES={}     # module_key -> [page_key]
MODULE_LABEL={}
for m in acc["modules"]:
    MODULE_LABEL[m["module_key"]]=m["label"]
    MODULE_PAGES[m["module_key"]]=[p["page_key"] for p in m["pages"]]
    for p in m["pages"]:
        PAGE_MODULE[p["page_key"]]=m["module_key"]

print(f"catalog: {len(MODULE_PAGES)} modules, {len(PAGE_MODULE)} tickable screens")
print(f"people:  {len(people)}\n")

def access_of(member):
    st,a=call(f"/admin/workforce/people/{member}/access", ceo_tok)
    return a if st==200 else None

def held(a):
    """modules held on web, and the pages ticked"""
    mods=set(); pages=set()
    for m in a["modules"]:
        if m["granted_web"]:
            mods.add(m["module_key"])
            pages |= set(m["granted_pages_web"])
    return mods, pages

def web(tok):
    st,d=call("/admin-web/bootstrap", tok)
    return st,d

def mobile(tok):
    st,d=call("/app/bootstrap", tok)
    return st,d

def leaves(d):
    out=[]
    for i in d["navigation"]["primary"]: out.append((i["id"],i["label"],i["href"],i.get("enabled",True)))
    for g in d["navigation"]["groups"]:
        for l in g["leaves"]: out.append((l["id"],l["label"],l["href"],l.get("enabled",True)))
    return out

# =====================================================================
# PASS 1 - every active person, web + mobile, read-only integrity
# =====================================================================
print("="*72)
print("PASS 1 - every person: web + mobile bootstrap integrity")
print("="*72)
VERIFIER_LENS=set()
summary=[]
for p in people:
    if not p["uid"]:
        summary.append((p["name"],p["roles"],"no login","-",""))
        continue
    tok=mint(p["uid"])
    if not tok:
        chk(False,f"{p['name']}: token", "mint returned empty"); continue
    ws,wd=web(tok)
    ms,md=mobile(tok)
    a=access_of(p["member"])
    hm,hp=held(a) if a else (set(),set())

    if ws==200:
        lv=leaves(wd)
        groups=[g["label"] for g in wd["navigation"]["groups"]]
        # is this the verifier workspace? (its own composed nav, exempt by decision)
        is_verifier = wd.get("pages") and len(wd["pages"])==1 and wd["pages"][0]["href"]=="/verify"
        if is_verifier: VERIFIER_LENS.add(p["name"])

        if not is_verifier and hm:
            # W2/W3: every visible leaf must be a page this person is ticked for
            for lid,lab,href,en in lv:
                if lid in PAGE_MODULE:
                    chk(lid in hp, f"{p['name']}: leaf '{lab}' visible",
                        f"page {lid} not ticked (ticked={sorted(hp)})")
            # W7: nothing rendered disabled - unticked pages are REMOVED, not greyed
            for lid,lab,href,en in lv:
                chk(en, f"{p['name']}: leaf '{lab}' not greyed", "rendered disabled")
            # W5: every leaf has a page contract (no dead link)
            contracts={c["href"] for c in wd["pages"]}
            for lid,lab,href,en in lv:
                base=href.split("?")[0]
                chk(base in contracts, f"{p['name']}: leaf '{lab}' has a contract", f"{base} missing")
            # THE USER'S RULE: a module not held must not appear AT ALL
            for mod,pgs in MODULE_PAGES.items():
                if not pgs or mod in hm: continue
                shown=[lab for lid,lab,_,_ in lv if lid in pgs]
                chk(not shown, f"{p['name']}: module '{MODULE_LABEL[mod]}' fully absent",
                    f"still shows {shown}")
        summary.append((p["name"],p["roles"],f"web {ws} ({len(lv)} leaves)",f"app {ms}", ", ".join(groups)))
    else:
        summary.append((p["name"],p["roles"],f"web {ws}",f"app {ms}",""))
    chk(ws in (200,403), f"{p['name']}: web bootstrap", f"status {ws}")
    chk(ms in (200,403), f"{p['name']}: mobile bootstrap", f"status {ms}")

print(f"{'PERSON':<22}{'ROLES':<48}{'WEB':<22}{'APP':<9}GROUPS")
for r in summary:
    print(f"{r[0][:21]:<22}{r[1][:47]:<48}{r[2]:<22}{r[3]:<9}{r[4][:60]}")
print(f"\nverifier workspace: {sorted(VERIFIER_LENS)}")
print(f"\nPASS 1: {P[0]} passed, {F[0]} failed")


print("="*74)
print("PASS 2 - grant / revoke, per persona, same token throughout (no re-login)")
print("="*74)

ROUTES={  # a representative route behind each module, to prove ticks remove ABILITY
 "sales":"/sales/overview","vendors":"/vendors","feed_purchases":"/feed-purchases",
 "procurement":"/procurement/source-entry/loads","feed_direction":"/feed-config/ration-groups",
 "people":"/admin/operators","weighing":"/weighing/weights-window",
}
def save(member, mutate):
    st,a=call(f"/admin/workforce/people/{member}/access", ceo_tok)
    if st!=200: return None,f"read {st}"
    mods=[]
    for m in a["modules"]:
        row={"module_key":m["module_key"],"web":list(m["granted_web"]),
             "mobile":list(m["granted_mobile"]),"pages":list(m["granted_pages_web"])}
        mutate(row,m); mods.append(row)
    body={"designation_code":a.get("designation_code",""),"scope_mode":a["scope_mode"],
          "park_ids":a["park_ids"],"modules":mods,"row_version":a["row_version"]}
    s2,_=call(f"/admin/workforce/people/{member}/access", ceo_tok, "PUT", body)
    return s2,None

def groups_of(d): return [g["label"] for g in d["navigation"]["groups"]]
def leaf_labels(d): return {l[1] for l in leaves(d)}

targets=[p for p in people if p["uid"] and p["roles"] not in ("(none)",)]
targets=[p for p in targets if p["roles"]!="verifier"]  # verifier keeps its own workspace by decision
# one representative per role-shape, plus every distinct shape
seen=set(); reps=[]
for p in targets:
    if p["roles"] in seen: continue
    seen.add(p["roles"]); reps.append(p)
print(f"testing {len(reps)} distinct role-shapes\n")

for p in reps:
    tok=mint(p["uid"])                      # minted ONCE, never refreshed
    st0,d0=web(tok)
    a0=access_of(p["member"]); hm0,_=held(a0)
    if st0!=200:
        # a person with no web modules must be refused the console entirely
        chk(not hm0, f"{p['name']}: web 403 only when no web module", f"holds {sorted(hm0)}")
        print(f"{p['name']:<14} {p['roles'][:34]:<36} web 403 (no web modules) - correct")
        continue
    base_groups=groups_of(d0); base_leaves=leaf_labels(d0)

    # pick a module this person actually holds that owns a visible group
    victim=None
    for mod in sorted(hm0):
        pgs=MODULE_PAGES.get(mod) or []
        vis=[l for l in leaves(d0) if l[0] in pgs]
        if vis: victim=(mod,[v[1] for v in vis]); break
    if not victim:
        print(f"{p['name']:<14} {p['roles'][:34]:<36} no visible module to mutate"); continue
    mod,labels=victim

    # --- REVOKE the whole module ---
    s,_=save(p["member"], lambda row,m,mod=mod: (row.update(web=[],pages=[]) if row["module_key"]==mod else None))
    chk(s==200, f"{p['name']}: revoke {mod} accepted", f"status {s}")
    st1,d1=web(tok)                          # SAME token - no logout
    if st1==200:
        gone=leaf_labels(d1)
        for lab in labels:
            chk(lab not in gone, f"{p['name']}: '{lab}' removed with {mod}", "still visible")
        # the user's rule: module fully absent, not greyed, not an empty heading
        for g in d1["navigation"]["groups"]:
            chk(len(g["leaves"])>0, f"{p['name']}: no empty group after revoke", f"'{g['label']}' empty")
        chk(all(l[3] for l in leaves(d1)), f"{p['name']}: nothing greyed after revoke","")
    else:
        chk(st1==403, f"{p['name']}: web refused when last module removed", f"status {st1}")
    if mod in ROUTES:
        rc,_=call(ROUTES[mod], tok)
        # Permissions UNION across surfaces: revoking the WEB row removes the web screens,
        # but the ability survives while the PHONE still grants the same module. Assert the
        # true rule, not a stricter one.
        still_mobile = any(m["module_key"]==mod and m["granted_mobile"] for m in (access_of(p["member"]) or {"modules":[]})["modules"])
        if still_mobile:
            chk(rc!=403, f"{p['name']}: {mod} API survives web revoke (still on phone)", f"status {rc}")
        else:
            chk(rc==403, f"{p['name']}: {mod} API refuses after revoke", f"status {rc}")
        # ...and revoking BOTH surfaces really does remove the ability.
        s2,_=save(p["member"], lambda row,m,mod=mod: (row.update(web=[],mobile=[],pages=[]) if row["module_key"]==mod else None))
        if s2==200:
            rc2,_=call(ROUTES[mod], tok)
            chk(rc2==403, f"{p['name']}: {mod} API refuses when revoked on BOTH surfaces", f"status {rc2}")
    # mobile must be untouched
    ms1,_=mobile(tok)
    chk(ms1 in (200,403), f"{p['name']}: mobile survives revoke", f"status {ms1}")

    # --- RESTORE ---
    orig={m["module_key"]:(m["granted_web"],m["granted_mobile"],m["granted_pages_web"]) for m in a0["modules"]}
    s,_=save(p["member"], lambda row,m,o=orig: row.update(web=list(o[row["module_key"]][0]),
                                                          mobile=list(o[row["module_key"]][1]),
                                                          pages=list(o[row["module_key"]][2])))
    chk(s==200, f"{p['name']}: restore accepted", f"status {s}")
    st2,d2=web(tok)
    chk(st2==200, f"{p['name']}: web back", f"status {st2}")
    if st2==200:
        chk(groups_of(d2)==base_groups, f"{p['name']}: groups restored", f"{groups_of(d2)} != {base_groups}")
        chk(leaf_labels(d2)==base_leaves, f"{p['name']}: leaves restored",
            f"missing {sorted(base_leaves-leaf_labels(d2))} extra {sorted(leaf_labels(d2)-base_leaves)}")
    if mod in ROUTES:
        rc,_=call(ROUTES[mod], tok)
        chk(rc!=403, f"{p['name']}: {mod} API reachable again", f"status {rc}")
    print(f"{p['name']:<14} {p['roles'][:34]:<36} revoke+restore '{mod}' ok  ({len(base_leaves)} leaves)")


# The data route each visible leaf depends on. A leaf whose route 403s is a DEAD SCREEN:
# it renders, the person clicks it, and it fails. That is the thing the tick must prevent.
LEAF_ROUTE={
 "control-tower":"/control-tower/vaccination","action-center":"/action-center/obligations",
 "calendar":"/calendar/vaccination/events","protocol-adherence":"/vaccination/adherence",
 "workflows":"/app/workflows","approvals":"/admin-web/counts/approvals",
 "verification-actions":"/verification/queue",
 "preventive-care-vaccination":"/vaccination/action-center",
 "vaccination-plan":"/protocols",
 "procurement-source-entry":"/procurement/source-entry/loads","procurement-vendors":"/procurement/vendors",
 "procurement-sales":"/sales/overview","procurement-feed-purchases":"/procurement/feed-purchases",
 "counts-herd-analytics":"/counts/herd-analytics","counts-breakdown":"/counts/breakdown",
 "counts-sops":"/admin/sops","milk-sops":"/admin/sops","feed-sops":"/admin/sops",
 "weighing-sops":"/admin/sops","milk-preparation":"/counts/milk-preparation",
 "herd-signals":"/herd-signals/live","weighing-weights":"/weighing/campaigns",
 "feed-config":"/feed-config/ration-groups","feed-analytics":"/feed-analytics/directed",
 "health-config":"/health-config/protocols","audit-log":"/operations/audit","dlq-center":"/operations/dlq",
 "people":"/admin/operators",
}
print("="*74)
print("PASS 3 - dead-screen sweep: every visible leaf, does its data load")
print("="*74)
unmapped=set(); dead=[]
for p in people:
    if not p["uid"]: continue
    tok=mint(p["uid"])
    st,d=web(tok)
    if st!=200: continue
    for lid,lab,href,en in leaves(d):
        r=LEAF_ROUTE.get(lid)
        if not r and lid.startswith("verification-"): r="/verification/queue"
        if not r: unmapped.add(lid); continue
        code,_=call(r, tok)
        ok = code!=403
        chk(ok, f"{p['name']}: '{lab}' data loads", f"{r} -> {code}")
        if not ok: dead.append((p["name"],lab,r,code))
print(f"checked leaves across {len([x for x in people if x['uid']])} principals")
if unmapped: print("unmapped leaf ids (no route asserted):", sorted(unmapped))
import time
print("="*74)
print("PASS 4 - reflection: does a change need a log out / log in?")
print("="*74)
H=[p for p in people if p["name"]=="Hemant"][0]
tok=mint(H["uid"])                      # ONE token, minted before any change, never refreshed
st,d=web(tok); before={l[1] for l in leaves(d)}
a0=access_of(H["member"])
print(f"  token minted once, before any change. baseline leaves: {len(before)}")

def put(mutate):
    a=access_of(H["member"])
    mods=[]
    for m in a["modules"]:
        r={"module_key":m["module_key"],"web":list(m["granted_web"]),
           "mobile":list(m["granted_mobile"]),"pages":list(m["granted_pages_web"])}
        mutate(r,m); mods.append(r)
    return call(f"/admin/workforce/people/{H['member']}/access", ceo_tok, "PUT",
        {"designation_code":a.get("designation_code",""),"scope_mode":a["scope_mode"],
         "park_ids":a["park_ids"],"modules":mods,"row_version":a["row_version"]})[0]

# grant a module he does not have at all
s=put(lambda r,m: r.update(web=[permissions_view], pages=["procurement-sales"]) if False else None)
# People at `view` produces operators.read, which is exactly what the People leaf needs.
s=put(lambda r,m: (r.update(web=["view"],pages=["people"]) if r["module_key"]=="people" else None))
chk(s==200, "grant accepted", f"status {s}")
t0=time.time(); seen=None
for _ in range(40):
    st,d=web(tok)                       # SAME token
    now={l[1] for l in leaves(d)}
    if "People / HRMS" in now: seen=time.time()-t0; break
    time.sleep(0.25)
chk(seen is not None, "granted module appears without re-login", "never appeared")
print(f"  GRANT  reflected in {seen:.2f}s on the SAME token (no logout, no new login)" if seen else "  GRANT never reflected")

s=put(lambda r,m: (r.update(web=[],pages=[]) if r["module_key"]=="people" else None))
t0=time.time(); gone=None
for _ in range(40):
    st,d=web(tok)
    if "People / HRMS" not in {l[1] for l in leaves(d)}: gone=time.time()-t0; break
    time.sleep(0.25)
chk(gone is not None, "revoked module disappears without re-login", "never disappeared")
print(f"  REVOKE reflected in {gone:.2f}s on the SAME token" if gone else "  REVOKE never reflected")

st,d=web(tok); after={l[1] for l in leaves(d)}
chk(after==before, "back to baseline", f"{sorted(after^before)}")

# ---- mobile: does a phone tick move the phone bar? (honest answer) ----
print("\n  mobile:")
ms,md=mobile(tok); m_before=sorted(x.get("key","?") for x in (md.get("modules") or []))
s=put(lambda r,m: (r.update(mobile=["view"]) if r["module_key"]=="weighing" else None))
time.sleep(1.5)
ms,md=mobile(tok); m_after=sorted(x.get("key","?") for x in (md.get("modules") or []))
print(f"    phone modules before tick: {m_before}")
print(f"    phone modules after  tick: {m_after}")
MOBILE_REFLECTS = m_after != m_before
print(f"    -> phone bar {'DOES' if MOBILE_REFLECTS else 'does NOT'} follow the phone tick yet")
s=put(lambda r,m,o={x['module_key']:x['granted_mobile'] for x in a0['modules']}: r.update(mobile=list(o[r["module_key"]])))
time.sleep(1.0)
ms,md=mobile(tok); m_end=sorted(x.get("key","?") for x in (md.get("modules") or []))
chk(m_end==m_before, "phone restored", f"{m_end} != {m_before}")
print("="*74)
print("PASS 5 - refusals: what the save must NOT accept")
print("="*74)
H=[p for p in people if p["name"]=="Hemant"][0]
def body(mutate, bump=0):
    a=access_of(H["member"]); mods=[]
    for m in a["modules"]:
        r={"module_key":m["module_key"],"web":list(m["granted_web"]),
           "mobile":list(m["granted_mobile"]),"pages":list(m["granted_pages_web"])}
        mutate(r,m); mods.append(r)
    return {"designation_code":a.get("designation_code",""),"scope_mode":a["scope_mode"],
            "park_ids":a["park_ids"],"modules":mods,"row_version":a["row_version"]+bump}
def put(b): return call(f"/admin/workforce/people/{H['member']}/access", ceo_tok, "PUT", b)[0]

cases=[
 ("a page from ANOTHER module", 400, lambda r,m: r.update(pages=["people"]) if r["module_key"]=="feed_direction" else None),
 ("an unknown page key",        400, lambda r,m: r.update(pages=["not-a-page"]) if r["module_key"]=="sales" else None),
 ("a granted module, no screen",400, lambda r,m: r.update(pages=[]) if r["module_key"]=="sales" else None),
 # Feed Config needs feed_config.read. Dropped to `view` on BOTH surfaces it is
 # unreachable, so the tick must be refused. (Permissions union across surfaces, so
 # lowering only the web row would leave the phone's `configure` still granting it --
 # the screen would genuinely open, and refusing would be wrong.)
 ("a screen no capability can open", 400,
                                     lambda r,m: r.update(web=["view"],mobile=["view"],pages=["feed-config"]) if r["module_key"]=="feed_direction" else None),
 ("an unknown module key",      400, lambda r,m: r.update(module_key="not_a_module") if r["module_key"]=="sales" else None),
 ("a capability the module does not offer", 400,
                                     lambda r,m: r.update(web=["configure"]) if r["module_key"]=="sales" else None),
 ("a level on a surface the module lacks", 400,
                                     lambda r,m: r.update(mobile=["view"]) if r["module_key"]=="sales" else None),
]
for label,want,mut in cases:
    got=put(body(mut))
    chk(got==want, f"refuses {label}", f"got {got} want {want}")
    print(f"  {'PASS' if got==want else 'FAIL'}  refuses {label}  -> {got}")
got=put(body(lambda r,m: None, bump=-1))
chk(got==409, "refuses a stale row_version", f"got {got}")
print(f"  {'PASS' if got==409 else 'FAIL'}  refuses a stale row_version -> {got}")
# a non-CEO must not be able to change anyone's access
op=[p for p in people if p["roles"]=="operator" and p["uid"]][0]
optok=mint(op["uid"])
r1=call(f"/admin/workforce/people/{H['member']}/access", optok)[0]
r2=call(f"/admin/workforce/people/{H['member']}/access", optok, "PUT", body(lambda r,m: None))[0]
chk(r1==403, "an operator cannot READ someone's access", f"got {r1}")
chk(r2==403, "an operator cannot CHANGE someone's access", f"got {r2}")
print(f"  {'PASS' if r1==403 else 'FAIL'}  operator read  -> {r1}")
print(f"  {'PASS' if r2==403 else 'FAIL'}  operator write -> {r2}")

# =====================================================================
# PASS 6 - the PHONE, per persona: does a tick move the bar and the ability
# =====================================================================
print("=" * 74)
print("PASS 6 - phone grant/revoke per persona, one token, no re-login")
print("=" * 74)

def phone_bar(tok):
    st, d = call("/app/bootstrap", tok)
    if st != 200:
        return st, []
    return st, sorted(m.get("key") for m in (d.get("modules") or []) if m.get("key"))

def save_person(member, mutate):
    st, a = call(f"/admin/workforce/people/{member}/access", ceo_tok)
    if st != 200:
        return st
    mods = []
    for m in a["modules"]:
        r = {"module_key": m["module_key"], "web": list(m["granted_web"]),
             "mobile": list(m["granted_mobile"]), "pages": list(m["granted_pages_web"])}
        mutate(r, m)
        mods.append(r)
    return call(f"/admin/workforce/people/{member}/access", ceo_tok, "PUT",
                {"designation_code": a.get("designation_code", ""), "scope_mode": a["scope_mode"],
                 "park_ids": a["park_ids"], "modules": mods, "row_version": a["row_version"]})[0]

seen_shape = set()
for p in people:
    if not p["uid"] or p["roles"] == "(none)":
        continue
    if p["roles"] in seen_shape:
        continue
    seen_shape.add(p["roles"])
    tok = mint(p["uid"])                       # ONE token, before any change
    st0, base = phone_bar(tok)
    if st0 != 200 or not base:
        print(f"  {p['name']:<14} no phone bar ({st0}) - skipped")
        continue
    a0 = access_of(p["member"])
    orig = {m["module_key"]: (list(m["granted_web"]), list(m["granted_mobile"]), list(m["granted_pages_web"]))
            for m in a0["modules"]}
    # pick a module that is actually ON the bar and is ticked on mobile
    victim = None
    for m in a0["modules"]:
        if m["granted_mobile"] and m["module_key"] in base:
            victim = m["module_key"]
            break
    if victim is None:
        print(f"  {p['name']:<14} bar is composed, not ticked (verifier/duty) - exempt, bar {base}")
        continue

    s = save_person(p["member"], lambda r, m, v=victim: r.update(mobile=[]) if r["module_key"] == v else None)
    chk(s == 200, f"{p['name']}: phone revoke of {victim} accepted", f"status {s}")
    st1, after = phone_bar(tok)                 # SAME token
    chk(victim not in after, f"{p['name']}: {victim} left the phone bar", f"bar still {after}")
    # The phone opens for anyone holding ANY mobile row -- that is what grants app.bootstrap.
    # A person can therefore lose their last BAR module and still open the app (Hemant holds
    # People on the phone with no phone module of its own), so the check is on the rows, not
    # on how many icons were showing.
    after_rows = access_of(p["member"]) or {"modules": []}
    mobile_left = any(m["granted_mobile"] for m in after_rows["modules"])
    if mobile_left:
        chk(st1 == 200, f"{p['name']}: phone still opens after revoke", f"status {st1}")
    else:
        chk(st1 == 403, f"{p['name']}: phone refused when no mobile access is left", f"status {st1}")

    fresh = mint(p["uid"])                      # a real log out + log in
    st2, after2 = phone_bar(fresh)
    chk(victim not in after2, f"{p['name']}: {victim} still gone after re-login", f"bar {after2}")

    s = save_person(p["member"], lambda r, m, o=orig: r.update(
        web=list(o[r["module_key"]][0]), mobile=list(o[r["module_key"]][1]), pages=list(o[r["module_key"]][2])))
    chk(s == 200, f"{p['name']}: phone restore accepted", f"status {s}")
    st3, back = phone_bar(tok)
    chk(back == base, f"{p['name']}: phone bar restored", f"{back} != {base}")
    print(f"  {p['name']:<14} {p['roles'][:30]:<32} revoke+restore '{victim}' ok  (bar {len(base)})")

# =====================================================================
# PASS 7 - capability enforcement: does a LEVEL actually limit what they can do
# =====================================================================
print("=" * 74)
print("PASS 7 - every person x every write route: the level decides, nothing else")
print("=" * 74)

# One WRITE route per permission, each gated on exactly that permission. A person holding
# `view` on a module must be REFUSED its write; a person holding `do`/`configure` must not be.
# The route is called with NO body, so a 4xx that is not 403 means the gate let them through
# and the handler rejected the payload -- which is the "allowed" answer for this test.
# The level -> permission map AND the write routes, both read from the SAME source the
# server resolves through (backend/cmd/print-capability-catalog). Nothing here restates a
# rule: a hand-copied permission string is what made a first run report six false failures
# on procurement.vendor.write.
_dump = json.loads(subprocess.run(
    ["go", "run", "./cmd/print-capability-catalog"], capture_output=True, text=True, env=ENV).stdout)
LEVEL_PERMS = {}
for _m in _dump["modules"]:
    for _lvl, _perms in _m["levels"].items():
        LEVEL_PERMS[(_m["key"], _lvl)] = _perms
# The surface-baseline permissions are DERIVED, never ticked: holding any module on a
# surface admits you to it. They gate no business write, so they are not part of this test.
_BASELINE = {"app.bootstrap", "admin_web.bootstrap", "locations.read", "task.read"}
WRITE_ROUTES = {r["permission"]: (r["method"], r["path"])
                for r in _dump["write_routes"] if r["permission"] not in _BASELINE}


checked = 0
for p in people:
    if not p["uid"]:
        continue
    tok = mint(p["uid"])
    a = access_of(p["member"])
    if not a:
        continue
    # what this person's ticks RESOLVE to, asked of the same catalog the server uses
    held = set()
    for m in a["modules"]:
        for surface in ("granted_web", "granted_mobile"):
            for lvl in m[surface]:
                held |= set(LEVEL_PERMS.get((m["module_key"], lvl), []))
    for perm, (method, path) in WRITE_ROUTES.items():
        code, _ = call(path, tok, method, {})
        allowed = code != 403
        should = perm in held
        chk(allowed == should,
            f"{p['name']}: {method} {path}",
            f"{'allowed' if allowed else 'refused'} ({code}) but the ticks say {'allowed' if should else 'refused'} for {perm}")
        checked += 1
print(f"  {checked} person x write-route checks")

print("")
print("=" * 74)
print(f"TOTAL: {P[0]} passed, {F[0]} failed")
print("=" * 74)
for n in NOTES:
    print("  FAIL:", n)
raise SystemExit(1 if F[0] else 0)
