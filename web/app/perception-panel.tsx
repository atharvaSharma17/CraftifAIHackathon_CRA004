'use client';

import { useEffect, useRef, useState } from 'react';

type Point = [number, number];
export type Warehouse = {
  nodes: { id:string; x:number; y:number; label:string }[];
  edges: { id:string; from:string; to:string; length:number; width:number }[];
  camera_id:string; calibration_version:string; calibration_verified:boolean;
  image_size:[number,number]; rois:{id:string; edge_ids:string[]; polygon:Point[]}[];
  vehicle_profiles:{id:string}[];
  policy:{strict_unknown:boolean; unknown_penalty:number; stale_after_ms:number; obstacle_classes:string[]; confidence_threshold:number};
};
export type Observation = {
  input_mode?:'replay'|'live'; frame_index:number; video_ts_ms:number; session_id:string;
  image_size:[number,number]; objects:{class_label:string; confidence:number; track_id?:string; bbox_xywh_px:[number,number,number,number]}[];
};
export type InputStatus = {mode:string; status:string; error?:string};

export default function PerceptionPanel({map,observation,input,connected,occupancy}:{
  map:Warehouse|null; observation?:Observation; input?:InputStatus; connected:boolean;
  occupancy:{roi_id:string;state:string}[];
}) {
  const video=useRef<HTMLVideoElement>(null);
  const capturedPreview=useRef('');
  const [preview,setPreview]=useState(''),[previewTime,setPreviewTime]=useState(0),[decodeFailed,setDecodeFailed]=useState(false);
  const [localURL,setLocalURL]=useState(''),[available,setAvailable]=useState(true),[sync,setSync]=useState(false),[videoError,setVideoError]=useState('');
  const [editing,setEditing]=useState(false),[points,setPoints]=useState<Point[]>([]),[edge,setEdge]=useState('B-C');
  const [verified,setVerified]=useState(false),[videoTime,setVideoTime]=useState(0);
  const [roiID,setRoiID]=useState('aisle_BC');
  useEffect(()=>()=>{if(localURL)URL.revokeObjectURL(localURL)},[localURL]);
  useEffect(()=>{
    if(sync && !editing && !decodeFailed && observation && video.current && video.current.readyState>=1){
      video.current.pause();
      if(observation.video_ts_ms/1000>video.current.duration){setVideoError('Video duration does not cover this observation. Choose the matching source recording.');return;}
      if(Math.abs(video.current.currentTime-observation.video_ts_ms/1000)>0.03)video.current.currentTime=observation.video_ts_ms/1000;
    }
  },[sync,editing,decodeFailed,observation]);
  const size=map?.image_size??[1920,1080];
  const aligned=observation && Math.abs((decodeFailed?previewTime:videoTime)-observation.video_ts_ms/1000)<0.15;
  const largeBoxes=observation?.objects.filter(o=>o.bbox_xywh_px[2]*o.bbox_xywh_px[3]>size[0]*size[1]*0.8).length??0;
  function capturePreview(){
    const v=video.current;if(!v||v.readyState<2||!v.videoWidth)return;
    try{const canvas=document.createElement('canvas');canvas.width=v.videoWidth;canvas.height=v.videoHeight;
      canvas.getContext('2d')?.drawImage(v,0,0);const url=canvas.toDataURL('image/jpeg',0.85);
      capturedPreview.current=url;setPreview(url);setPreviewTime(v.currentTime);
    }catch{/* A preview failure must not interrupt perception or routing. */}
  }
  function downloadCalibration(){
    if(!map||points.length<3)return;
    const config={...map,calibration_version:`camera-calibration-${Date.now()}`,calibration_verified:verified,
      rois:[...map.rois.filter(r=>r.id!==roiID),{id:roiID,edge_ids:[edge],polygon:points}]};
    const url=URL.createObjectURL(new Blob([JSON.stringify(config,null,2)],{type:'application/json'}));
    const a=document.createElement('a');a.href=url;a.download='warehouse.calibrated.json';a.click();setTimeout(()=>URL.revokeObjectURL(url),1000);
  }
  return <section className="panel" id="perception">
    <div className="panel-heading"><div><h2>Perception bridge</h2><p>PipeGen → observations → occupancy</p></div><span className={`status-dot ${connected?'green':'amber'}`}/></div>
    <div className="perception-body">
      <div className="video-stage" style={{aspectRatio:`${size[0]} / ${size[1]}`}}>
        <video ref={video} src={localURL||`/api/video?run=${encodeURIComponent(observation?.session_id??map?.calibration_version??'preview')}`} controls={!editing} muted playsInline
          onLoadedMetadata={()=>{setAvailable(true);setDecodeFailed(false);setVideoError('');if(sync&&observation&&video.current){if(observation.video_ts_ms/1000<=video.current.duration)video.current.currentTime=observation.video_ts_ms/1000;else setVideoError('Video duration does not cover this observation. Choose the matching source recording.');}}}
          onLoadedData={capturePreview} onSeeked={capturePreview}
          onError={()=>{setDecodeFailed(true);setAvailable(Boolean(capturedPreview.current));setVideoError(capturedPreview.current?'Video decoding failed. Showing the last decoded still frame for inspection. Choose the matching original MP4 for playback.':'This MP4 could not be decoded. Choose the matching original source MP4 instead.')}} onTimeUpdate={()=>setVideoTime(video.current?.currentTime??0)}/>
        {decodeFailed&&preview&&<img className="video-still" src={preview} alt={`Last decoded camera frame at ${previewTime.toFixed(2)} seconds`}/>}
        {!available&&<div className="video-empty">Choose the matching MP4 to inspect detections and calibrate visible lanes.</div>}
        <svg viewBox={`0 0 ${size[0]} ${size[1]}`} className={editing?'calibration-overlay':'detection-overlay'}
          aria-label={editing?'Click visible floor corners to define the selected lane polygon':'Camera regions and timestamp-aligned detections'}
          onClick={e=>{if(!editing||!available)return;const r=e.currentTarget.getBoundingClientRect();setPoints(p=>[...p,[Math.round((e.clientX-r.left)/r.width*size[0]),Math.round((e.clientY-r.top)/r.height*size[1])]]);setVerified(false)}}>
          {map?.rois.filter(r=>!editing||r.id!==roiID).map(r=><g key={r.id}><polygon points={r.polygon.map(p=>p.join(',')).join(' ')} fill="#b0f37b18" stroke="#b0f37b" strokeWidth="4"/><text x={r.polygon[0][0]} y={r.polygon[0][1]-12} fill="#b0f37b" fontSize="30">{r.id} / {r.edge_ids.join(', ')}</text></g>)}
          {aligned&&!editing&&observation?.objects.map((o,i)=><g key={i}><rect x={o.bbox_xywh_px[0]} y={o.bbox_xywh_px[1]} width={o.bbox_xywh_px[2]} height={o.bbox_xywh_px[3]} fill="none" stroke="#f4c16d" strokeWidth="4"/><text x={o.bbox_xywh_px[0]+6} y={Math.max(32,o.bbox_xywh_px[1]+32)} fill="#f4c16d" stroke="#101619" strokeWidth="1" fontSize="28">{o.class_label} {Math.round(o.confidence*100)}% #{o.track_id??'?'}</text><circle cx={o.bbox_xywh_px[0]+o.bbox_xywh_px[2]/2} cy={o.bbox_xywh_px[1]+o.bbox_xywh_px[3]} r="8" fill="#ff6f65"/></g>)}
          {editing&&<><polyline points={points.map(p=>p.join(',')).join(' ')} fill="#b0f37b22" stroke="#b0f37b" strokeWidth="5"/>{points.map((p,i)=><circle key={i} cx={p[0]} cy={p[1]} r="10" fill="#b0f37b"/>)}</>}
        </svg>
      </div>
      <div className="perception-controls"><label className="video-upload">Choose matching MP4<input type="file" accept="video/mp4,video/webm" onChange={e=>{const f=e.target.files?.[0];if(f){capturedPreview.current='';setPreview('');setDecodeFailed(false);setLocalURL(URL.createObjectURL(f));setAvailable(true);setVideoError('')}}}/></label><label><input type="checkbox" checked={sync} disabled={decodeFailed} onChange={e=>setSync(e.target.checked)}/> Follow metadata time</label></div>
      {videoError&&<p role="alert" className="input-error">{videoError}</p>}
      <p className="small-note">{sync?'Video seeks to the latest metadata timestamp.':'Independent playback; boxes appear only when timestamps match.'} Incoming metadata drives routing. {observation?.input_mode==='replay'?'Recorded inference replay.':''}</p>
      {input?.error&&<p role="alert" className="input-error">{input.error}</p>}
      {largeBoxes>0&&<p className="input-error">{largeBoxes} detection(s) cover over 80% of this frame. Check model/parser localization before verifying calibration.</p>}
      <div className="key-value"><span>Input</span><span>{input?.mode||observation?.input_mode||'External bridge'} · {input?.status|| (connected?'receiving':'waiting')}</span></div>
      <div className="key-value"><span>Camera / resolution</span><code>{map?.camera_id??'cam01'} / {size.join('×')}</code></div>
      <div className="key-value"><span>Calibration</span><span>{map?.calibration_verified?'Verified':'Unverified'}</span></div>
      <div className="key-value"><span>Frame / video time</span><span>{observation?`${observation.frame_index} / ${(observation.video_ts_ms/1000).toFixed(2)} s`:'No frames'}</span></div>
      <div className="key-value"><span>Objects / policy classes</span><span>{observation?.objects.length??0} / {map?.policy.obstacle_classes.join(', ')}</span></div>
      {occupancy.map(r=><div className="key-value" key={r.roi_id}><span>{r.roi_id}</span><span>{r.state.toLowerCase().replaceAll('_',' ')}</span></div>)}
      <button className="button secondary calibration-toggle" disabled={!map||!available} onClick={()=>{setEditing(v=>!v);setVerified(false);video.current?.pause()}}>{editing?'Close calibration editor':'Calibrate visible lane'}</button>
      {editing&&<div className="calibration-editor">
        <p>Pause on a clear view. Click floor corners around a visible lane, in order. Verify that the selected graph edge represents that floor region.</p>
        <label>Region<select value={roiID} onChange={e=>{setRoiID(e.target.value);setPoints([]);setVerified(false);const r=map?.rois.find(r=>r.id===e.target.value);if(r)setEdge(r.edge_ids[0])}}>{map?.rois.map(r=><option key={r.id}>{r.id}</option>)}</select></label>
        <label>Graph lane<select value={edge} onChange={e=>{setEdge(e.target.value);setVerified(false)}}>{map?.edges.map(e=><option key={e.id}>{e.id}</option>)}</select></label>
        <div className="perception-controls"><span>{points.length} corners</span><button className="button" onClick={()=>{setPoints(p=>p.slice(0,-1));setVerified(false)}}>Undo corner</button><button className="button" onClick={()=>{setPoints([]);setVerified(false)}}>Clear</button></div>
        <label className="verify-label"><input type="checkbox" checked={verified} onChange={e=>setVerified(e.target.checked)}/> I checked all configured regions, lane associations, and detection localization against this footage.</label>
        <button className="button primary" disabled={points.length<3} onClick={downloadCalibration}>Download {verified?'verified':'draft'} configuration</button>
        <p className="small-note">Restart with this downloaded file using <code>-Config</code>. Calibration remains unverified until you check it.</p>
      </div>}
    </div>
  </section>;
}
