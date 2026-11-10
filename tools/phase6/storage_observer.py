#!/usr/bin/env python3
"""Diagnostic-only fsync timing and proc counters; never an acceptance gate."""
import collections
import json
import os
from pathlib import Path
import sys
import time

if sys.argv[1] == 'fsync':
    root = Path(sys.argv[2])
    path = root / 'fsync-probe.bin'
    samples = []
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    try:
        for _ in range(500):
            assert os.write(fd, b'x' * 4096) == 4096
            wall, cpu = time.monotonic_ns(), time.process_time_ns()
            os.fsync(fd)
            samples.append(dict(wall_ns=time.monotonic_ns()-wall, cpu_ns=time.process_time_ns()-cpu))
    finally:
        os.close(fd)
        path.unlink()
    values = sorted(s['wall_ns'] for s in samples)
    print(json.dumps(dict(scope='diagnostic; real fsync, no latency gate', samples=samples,
                         p50_ns=values[249], p99_ns=values[494], max_ns=values[-1])))
else:
    assert sys.argv[1] == 'processes'
    while True:
        now = time.monotonic_ns()
        for path in Path('/proc').iterdir():
            if not path.name.isdecimal():
                continue
            try:
                exe = (path / 'exe').resolve().name
                if exe not in ['vortex-node', 'vortex-test-hub', 'agent.test']:
                    continue
                stat = (path / 'stat').read_text().split(') ', 1)[1].split()
                io = dict(line.split(':', 1) for line in (path / 'io').read_text().splitlines())
                waits = collections.Counter()
                scheduled = queued = 0
                for thread in (path / 'task').iterdir():
                    try:
                        waits[(thread / 'wchan').read_text().strip()] += 1
                        cpu, delay, _ = map(int, (thread / 'schedstat').read_text().split())
                        scheduled += cpu
                        queued += delay
                    except (OSError, ValueError):
                        pass
                print(json.dumps(dict(monotonic_ns=now, pid=int(path.name), exe=exe,
                                      state=stat[0], user_ticks=int(stat[11]), system_ticks=int(stat[12]),
                                      ticks_per_second=os.sysconf('SC_CLK_TCK'),
                                      scheduled_ns=scheduled, runqueue_ns=queued,
                                      waits=waits, io={k:int(v) for k,v in io.items()})), flush=True)
            except (OSError, ValueError):
                pass
        time.sleep(0.1)
