import json,re,collections,os,pathlib

# reads and writes beside this file by default; override with WEIGHING_DATA_DIR
DATA_DIR=pathlib.Path(os.environ.get('WEIGHING_DATA_DIR','.'))
NEG=json.load(open(DATA_DIR/'classified.json'))
POS=json.load(open(DATA_DIR/'pos_events.json'))
# map: (goat_id, date) -> reason_code of an INVALID low reading
badlow={}
for e in NEG:
    if e['verdict']=='invalid':
        badlow[(e['goat_id'],e['cur_date'])]=e['reason_code']
# map of valid losses ending at prev_date -> compensatory context
lossreason={}
for e in NEG:
    lossreason[(e['goat_id'],e['cur_date'])]=(e['verdict'],e['reason_code'])

def f(x):
    try:return float(x)
    except:return None
def i(x):
    try:return int(x)
    except:return 0
def digits(x): return ''.join(sorted(re.sub(r'[^0-9]','',('%g'%x))))
def within_noise(pct,days):
    return abs(pct) <= (6.0 if days<=3 else 3.0)

SICK=re.compile(r'(?i)health|fever|sick|weak|treat|icu|diarr|pneumo|wound|infect|scour')
KID_CEIL, KID_HARD = 0.35, 0.60      # kg/day for a growing kid
ADULT_CEIL, ADULT_HARD = 0.15, 0.30  # a mature animal lays down far less

def limits(e):
    return (ADULT_CEIL, ADULT_HARD) if (e.get('goat_type') or '').upper().startswith('ADL') else (KID_CEIL, KID_HARD)

def classify(e):
    CEIL, HARD = limits(e)
    pw,cw,nw=f(e['prev_wt']),f(e['cur_wt']),f(e['next_wt'])
    days=i(e['days']); adg=f(e['adg']) or 0.0
    delta=cw-pw; pct=delta/pw*100 if pw else 0
    gid=e['goat_id']
    idfmt=('positional_pen' if re.search(r'(?i)part.*id *\d+',gid) else
           'rfid15' if re.fullmatch(r'\d{14,16}',gid.strip()) else
           'numeric_tag' if re.fullmatch(r'\d{1,5}',gid.strip()) else
           'ID-n' if re.fullmatch(r'(?i)ID *- *\d+',gid.strip()) else 'other')
    shift=e.get('shift_detail') or ''

    # the previous reading was itself flagged bad -> this "gain" is the correction
    contradicted = (nw and pw and cw and cw>pw*1.12 and nw<=pw*1.05) or \
                   (i(e['same_day_rows'])>1 and (f(e['same_day_spread']) or 0)>2)
    if (gid,e['prev_date']) in badlow and not contradicted:
        return ('invalid','correction_of_prior_error',0.9,
          f'The {e["prev_date"]} reading was already flagged as bad data ({badlow[(gid,e["prev_date"])].replace("_"," ")}). '
          f'This is the number returning to normal, not the animal gaining {round(delta,1)}kg.',idfmt,pct)

    if pw and cw:
        r=cw/pw
        if 8.5<=r<=11.5:
            return ('invalid','decimal_shift_up',0.95,
              f'{cw}kg is almost exactly 10× {pw}kg — a decimal point added, or the earlier value lost one.',idfmt,pct)
        if digits(pw)==digits(cw) and abs(delta)>1.5:
            return ('invalid','digit_transposition',0.9,
              f'{pw} and {cw} use the same digits in a different order — a keystroke swap.',idfmt,pct)
        s_p,s_c=('%g'%pw),('%g'%cw)
        if len(s_c)>len(s_p) and s_c.endswith(s_p) and delta>3:
            return ('invalid','added_leading_digit',0.85,
              f'"{s_c}" is "{s_p}" with an extra digit in front — a double keypress.',idfmt,pct)
        if abs(delta-10.0)<0.05 and days<=21:
            return ('invalid','keypad_tens_slip',0.7,
              f'Exactly 10.0 kg gained in {days} days — tens-column keypad slip.',idfmt,pct)
    if i(e['same_day_rows'])>1 and (f(e['same_day_spread']) or 0)>2:
        return ('invalid','same_day_conflict',0.85,
          f'Two different weights filed against this ID on {e["cur_date"]}, {e["same_day_spread"]}kg apart.',idfmt,pct)
    # spike then fall straight back
    if nw and pw and cw>pw*1.12 and nw<=pw*1.05:
        return ('invalid','spike_then_drop',0.85,
          f'Jumped to {cw}kg then fell back to {nw}kg by {e["next_date"]}. Real growth does not reverse. '
          f'The {e["cur_date"]} reading is the wrong one.',idfmt,pct)

    # ---- valid gains ----
    tag, ptag = (e.get('shed_tag') or '').upper(), (e.get('ptag') or '').upper()
    if ptag=='ICU' and tag!='ICU' and adg<=HARD:
        return ('valid','icu_recovery',0.9,
                f'Came out of ICU into {tag or "the flock"} between these weighings — the shed tag says so. '
                'This is catch-up growth after recovery, not an error.',idfmt,pct)
    STAGES=('K1','K2','K3','K4','F1','F2')
    if ptag in STAGES and tag in STAGES and ptag!=tag and adg<=HARD:
        return ('valid','stage_move',0.8,
                f'Moved from {ptag} to {tag} between these weighings — a new stage brings a new ration.',idfmt,pct)

    prev = lossreason.get((gid,e['prev_date']))
    if prev and prev[0]=='valid' and prev[1] in ('illness_treatment','illness_shifting','post_kidding','abortion','weaning_stage_change'):
        if adg<=HARD:
            return ('valid','compensatory_regain',0.8,
              f'Follows a documented {prev[1].replace("_"," ")} — this is catch-up growth after the animal recovered.',idfmt,pct)
    if prev and prev[0]=='valid' and prev[1]=='transport_shed_change' and days<=21 and adg<=CEIL:
        return ('valid','refill_after_move',0.7,
          'Follows a shed move. Gut fill and hydration coming back, not new tissue.',idfmt,pct)
    if within_noise(pct,days):
        return ('valid','within_noise',0.7,
          f'{round(pct,1)}% over {days} days — gut fill and time-of-day, inside scale noise.',idfmt,pct)

    if adg>HARD:
        return ('invalid','implausible_gain',0.85,
          f'{round(adg,3)} kg/day over {days} days. No animal lays down weight that fast — the ceiling here '
          f'is about {CEIL} kg/day. Suspect a mis-keyed value or the wrong animal under this tag.',idfmt,pct)
    if adg>CEIL:
        if idfmt in ('positional_pen','ID-n'):
            return ('invalid','identity_drift',0.75,
              f'{round(adg,3)} kg/day is above the {CEIL} kg/day ceiling, and ID "{gid}" is a pen slot that gets reused — '
              'the two weighings are probably two different animals.',idfmt,pct)
        return ('unexplained','above_growth_ceiling',0.5,
          f'{round(adg,3)} kg/day is above the {CEIL} kg/day realistic ceiling but not impossible. '
          'Worth a spot-check before it is counted as growth.',idfmt,pct)
    if SICK.search(shift):
        return ('valid','recovery_after_care',0.7,
          f'Moved for care in this window ({shift[:120]}) and gained — recovery.',idfmt,pct)
    return ('valid','normal_growth',0.8,
      f'+{round(adg*1000)} g/day over {days} days — normal growth for this stage.',idfmt,pct)

out=[]
for e in POS:
    v,code,conf,expl,idfmt,pct=classify(e)
    o=dict(e); o.update(verdict=v,reason_code=code,confidence=conf,explanation=expl,
                        id_format=idfmt,pct_bw=round(pct,2),direction='gain')
    out.append(o)
json.dump(out,open(DATA_DIR/'classified_pos.json','w'))
print('total',len(out))
print(collections.Counter(o['verdict'] for o in out))
for k,n in collections.Counter(o['reason_code'] for o in out).most_common(): print(f'  {k:26s} {n}')
