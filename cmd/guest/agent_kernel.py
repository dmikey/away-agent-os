import sys,json,io,contextlib,traceback,os
out=sys.stdout
scope={'__name__':'__main__'}
class Capture(io.StringIO):
 def write(self,value):
  if self.tell()<18000: super().write(value[:18000-self.tell()])
  return len(value)
for line in sys.stdin:
 capture=Capture();failed=False
 try:
  job=json.loads(line);os.chdir('/workspace')
  with contextlib.redirect_stdout(capture),contextlib.redirect_stderr(capture):
   if job.get('filePath'):scope['__file__']=job['filePath']
   exec(compile(job['source'],job.get('filePath') or '<away-cell>','exec'),scope,scope)
 except BaseException:
  failed=True;capture.write(traceback.format_exc())
 out.write(json.dumps({'output':capture.getvalue(),'error':failed})+'\n');out.flush()
