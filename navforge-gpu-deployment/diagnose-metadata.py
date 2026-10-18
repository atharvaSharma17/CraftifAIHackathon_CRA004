#!/usr/bin/env python3
import json,sys,collections
p=sys.argv[1] if len(sys.argv)>1 else 'results/metadata.json'
with open(p) as f: data=json.load(f)
frames=data if isinstance(data,list) else data.get('frames',[])
counts=collections.Counter(str(o.get('class',o.get('label',o.get('class_id','unknown')))) for frame in frames for o in frame.get('objects',[]))
print('Frames:',len(frames),'Frames with detections:',sum(bool(f.get('objects')) for f in frames),'Detections:',sum(counts.values()))
print('Classes:',dict(counts))
if frames: print('First frame:',json.dumps(frames[0])[:500])
