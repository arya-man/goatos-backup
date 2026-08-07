import json, re, math, collections, os, pathlib

# reads and writes beside this file by default; override with WEIGHING_DATA_DIR
DATA_DIR=pathlib.Path(os.environ.get('WEIGHING_DATA_DIR','.'))
D=json.load(open(DATA_DIR/'neg_events.json'))
def f(x):
    try: return float(x)
    except: return None
def i(x):
    try: return int(x)
    except: return 0

def within_noise(pct,days):
    return abs(pct) <= (6.0 if days<=3 else 3.0)

SICK=re.compile(r'(?i)health|fever|sick|weak|treat|icu|diarr|loose motion|pneumo|cough|wound|lame|infect|scour|bloat|worm|deworm|mange|abscess|anaemi|anemi')
WEAN=re.compile(r'(?i)K0\s*(->|→|to)\s*K1|K1\s*(->|→|to)\s*K2|K2\s*(->|→|to)\s*K3|weaning|wean|K0\s*(->|→)\s*Mother|mother\s*(->|→)')
STAGE=re.compile(r'(?i)K[0-3]\s*(->|→|to)\s*(K[0-3]|F[12]|Mother)')
MOVE=re.compile(r'(?i)sold from|purchased from|shifted from .* to |CPT to CBE|CBE to CPT|new shed|swap|space')
BREED=re.compile(r'(?i)breeding|mating|delivery|kidding|sponge')

def digits(x):
    return ''.join(sorted(re.sub(r'[^0-9]','',('%g'%x))))

def classify(e):
    pw,cw=f(e['prev_wt']),f(e['cur_wt']); nw=f(e['next_wt'])
    days=i(e['days']); adg=f(e['adg']) or 0.0
    delta=cw-pw; pct=delta/pw*100 if pw else 0
    ev=[]; 
    shift=e.get('shift_detail') or ''
    health=e.get('health_detail') or ''
    idfmt='positional_pen' if re.search(r'(?i)part.*id *\d+',e['goat_id']) else \
          'rfid15' if re.fullmatch(r'\d{14,16}',e['goat_id'].strip()) else \
          'numeric_tag' if re.fullmatch(r'\d{1,5}',e['goat_id'].strip()) else \
          'ID-n' if re.fullmatch(r'(?i)ID *- *\d+',e['goat_id'].strip()) else 'other'

    # ---------- INVALID (data-entry) signatures, strongest first ----------
    if pw and cw:
        r=cw/pw
        if 0.085<=r<=0.115:
            return ('invalid','decimal_shift_down',0.95,
                    f'{cw}kg is almost exactly 1/10 of {pw}kg — decimal point dropped (operator typed {cw} for {round(pw,1)}).',ev,idfmt,pct)
        if digits(pw)==digits(cw) and abs(delta)>1.5:
            return ('invalid','digit_transposition',0.9,
                    f'{cw} and {pw} use the same digits — transposed keystrokes (e.g. {pw}→{cw}).',ev,idfmt,pct)
        s_p=('%g'%pw); s_c=('%g'%cw)
        if len(s_p)>len(s_c) and s_p.endswith(s_c) and abs(delta)>3:
            return ('invalid','dropped_leading_digit',0.85,
                    f'"{s_c}" is "{s_p}" with the leading digit missing — first keypress lost.',ev,idfmt,pct)
        if abs(abs(delta)-10.0)<0.05 and days<=21:
            return ('invalid','keypad_tens_slip',0.7,
                    f'Exactly 10.0 kg lost in {days} days — classic tens-column keypad slip, not biology.',ev,idfmt,pct)
    if i(e['same_day_rows'])>1 and (f(e['same_day_spread']) or 0)>2:
        return ('invalid','same_day_conflict',0.85,
                f'Two different weights recorded for this ID on {e["cur_date"]} ({e["same_day_spread"]}kg apart) — same tag scanned for two animals, or double entry.',ev,idfmt,pct)

    # V-shape rebound: middle reading is the outlier
    if nw and pw and nw>=pw*0.97 and cw<pw*0.9:
        return ('invalid','v_shape_outlier',0.85,
                f'Weight fell to {cw}kg then rebounded to {nw}kg by {e["next_date"]} — an animal cannot regain that. The {e["cur_date"]} reading is the bad one.',ev,idfmt,pct)

    # ---------- VALID reasons ----------
    tag, ptag = (e.get('shed_tag') or '').upper(), (e.get('ptag') or '').upper()
    if 'ICU' in (tag, ptag):
        where = 'went into ICU' if tag=='ICU' else 'was in ICU at the previous weighing'
        return ('valid','icu_stay',0.9,
                f'The animal {where} between these two weighings — the shed tag says so. '
                'An animal under care goes off feed and loses condition; this is the loss you expect.',
                ev,idfmt,pct)
    STAGES=('K1','K2','K3','K4','F1','F2')
    if ptag in STAGES and tag in STAGES and ptag!=tag:
        return ('valid','stage_move',0.85,
                f'Moved from {ptag} to {tag} between these weighings. A stage move changes the ration and, '
                'at weaning, removes milk — a real and temporary check on growth.',ev,idfmt,pct)
    if ptag in ('PREGNANT','MOTHER') and tag in ('NON-PREGNANT','NON PREGNANT','MILKING','F2'):
        return ('valid','kidded_by_tag',0.85,
                f'Tag moved from {ptag} to {tag} — the animal kidded between these weighings. '
                'Kid, placenta and fluids gone, then lactation drain.',ev,idfmt,pct)
    if tag=='MILKING' or ptag=='MILKING':
        return ('valid','lactation',0.8,
                'Tagged milking across this window — lactation draws condition off the doe.',ev,idfmt,pct)

    if i(e['abortion_n'])>0 and abs(pct)<=20:
        return ('valid','abortion',0.9,'Abortion recorded in this window — foetal and fluid loss explains the drop.',ev,idfmt,pct)
    if i(e['kidding_n'])>0:
        kd=e.get('kidding_date') or ''
        inwin = bool(kd) and kd > (e['prev_date'] or '')
        cap = 20.0 if inwin else 12.0
        if abs(pct) <= cap:
            return ('valid','post_kidding',0.95 if inwin else 0.7,
                (f'Kidded on {kd}, between these two weighings — loss of kid, placenta and fluids, then lactation drain.'
                 if inwin else
                 f'Kidded on {kd}, shortly before the first of these weighings — lactation is still draining condition.'),
                ev,idfmt,pct)
    if i(e['health_n'])>0 or SICK.search(health):
        det=(health or '')[:180]
        return ('valid','illness_treatment',0.9,
                f'Health record in window: {det} — sick/ICU animals go off feed and lose condition.',ev,idfmt,pct)
    if SICK.search(shift):
        return ('valid','illness_shifting',0.8,
                f'Moved for health reasons: {shift[:180]} — separated as weak/sick, weight loss expected.',ev,idfmt,pct)
    if WEAN.search(shift) or STAGE.search(shift):
        return ('valid','weaning_stage_change',0.8,
                f'Stage transition in window: {shift[:180]} — milk withdrawal at weaning causes a real, temporary check in growth.',ev,idfmt,pct)
    if BREED.search(shift):
        return ('valid','breeding_cycle',0.6,
                f'Breeding-related movement: {shift[:160]} — mating/late gestation shifts intake and condition.',ev,idfmt,pct)
    if e['shed']!=e['pshed'] and abs(pct)<12:
        return ('valid','transport_shed_change',0.6,
                f'Moved {e["pshed"]} → {e["shed"]} between weighings — transport/regrouping shrink (gut fill + stress) of {round(pct,1)}%.',ev,idfmt,pct)
    if idfmt in ('positional_pen','ID-n') and adg < -0.30:
        return ('invalid','identity_drift',0.75,
                f'ID "{e["goat_id"]}" names a pen slot, not an animal, and the drop of {round(adg,3)} kg/day is far past '
                'what an animal sustains. Two weighings under this label are most likely two different kids.',ev,idfmt,pct)
    if within_noise(pct,days):
        return ('valid','within_noise',0.7,
                f'Only {round(pct,1)}% of body weight over {days} day{"" if days==1 else "s"} — inside gut-fill, '
                'time-of-day and scale variation. Not a real loss.',ev,idfmt,pct)

    # ---------- Unexplained ----------
    if adg<-0.30:
        return ('invalid','implausible_rate',0.7,
                f'{round(adg,3)} kg/day over {days} days ({round(pct,1)}% of body weight) with no health, kidding or movement record — biologically implausible; suspect mis-keyed value or wrong tag.',ev,idfmt,pct)
    return ('unexplained','no_recorded_cause',0.4,
            f'{round(pct,1)}% loss over {days} days with no health, breeding, movement or stage record to explain it. Needs operator follow-up.',ev,idfmt,pct)

out=[]
for e in D:
    v,code,conf,expl,_,idfmt,pct=classify(e)
    e2=dict(e); e2.update(verdict=v,reason_code=code,confidence=conf,explanation=expl,id_format=idfmt,pct_bw=round(pct,2))
    out.append(e2)
json.dump(out,open(DATA_DIR/'classified.json','w'))
print('total',len(out))
print(collections.Counter(o['verdict'] for o in out))
for k,n in collections.Counter(o['reason_code'] for o in out).most_common(): print(f'  {k:26s} {n}')
