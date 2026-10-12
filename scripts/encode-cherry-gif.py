"""Convert the native UI recording to GIF without changing playback speed."""
import json
from pathlib import Path
import subprocess

root = Path(__file__).resolve().parents[1]
evidence = root / '.cache/cherry-evidence'
recording = json.loads((evidence / 'recording.json').read_text(encoding='utf-8'))
report = json.loads((evidence / 'report.json').read_text(encoding='utf-8'))
assert report['success'] and not report['rendererErrors']
assert 5 < recording['durationSeconds'] < 25
output = evidence / 'fakeapi-streaming.gif'
subprocess.run(['ffmpeg', '-hide_banner', '-loglevel', 'error', '-y',
                '-f', 'concat', '-safe', '0', '-i', str(evidence / 'frames.ffconcat'),
                '-filter_complex', '[0:v]fps=8,scale=864:-1:flags=lanczos,split[a][b];'
                '[a]palettegen=stats_mode=diff:max_colors=128[p];'
                '[b][p]paletteuse=dither=bayer:bayer_scale=4:diff_mode=rectangle',
                '-loop', '0', str(output)], check=True, timeout=120)
probe = json.loads(subprocess.check_output(['ffprobe', '-v', 'error', '-show_entries',
                   'stream=width,height,nb_frames:format=duration,size', '-of', 'json', str(output)], text=True))
assert abs(float(probe['format']['duration']) - recording['durationSeconds']) < 0.6
assert int(probe['format']['size']) < 5 * 1024 * 1024
assert int(probe['streams'][0]['nb_frames']) > 30
(evidence / 'gif-metadata.json').write_text(json.dumps(probe, indent=2) + '\n', encoding='utf-8')
print('PASS real-time GIF encoding:', probe['format']['duration'], 'seconds,', probe['format']['size'], 'bytes')
