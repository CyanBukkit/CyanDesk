import json, urllib.request, sys

# Get latest 5 runs
req = urllib.request.Request('https://api.github.com/repos/CyanBukkit/CyanDesk/actions/runs?per_page=5&order=desc')
d = json.load(urllib.request.urlopen(req))

for run in d['workflow_runs']:
    sha = run['head_sha'][:7]
    name = run['name']
    conc = run['conclusion']
    status = run['status']
    print(f"\n=== {name} | {sha} | {conc}/{status} ===")

    # Get jobs
    try:
        j = json.load(urllib.request.urlopen(run['jobs_url']))
        for job in j['jobs']:
            jname = job['name']
            jconc = job['conclusion']
            print(f"  📋 {jname}: {jconc}")
            for s in job.get('steps', []):
                num = s['number']
                step_name = s['name']
                sconc = s.get('conclusion') or ''
                if sconc in ('failure', 'cancelled', 'skipped') and num > 1:
                    print(f"    ❌ {num}. {step_name}: {sconc}")
    except Exception as e:
        print(f"  jobs error: {e}")
