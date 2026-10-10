import torch, sys, numpy as np, soundfile as sf
sys.path.insert(0, '/tmp/umx/msst')
from models.bs_roformer.bs_roformer import BSRoformer

model = BSRoformer(dim=256, depth=12, heads=8, stereo=True, dim_head=64,
    time_transformer_depth=1, freq_transformer_depth=1,
    dim_freqs_in=1025, stft_n_fft=2048, stft_hop_length=512,
    mask_estimator_depth=2, num_stems=1)
sd = torch.load('/tmp/umx/bs_roformer_karaoke.ckpt', map_location='cpu')
model.load_state_dict(sd.get('state_dict', sd)); model.eval()

audio, sr = sf.read('/tmp/umx/input.wav', dtype='float32', always_2d=True)  # (T,2)
audio = audio.T  # (2,T)
SR = 44100; CHUNK = 3 * SR; OVERLAP = SR // 2
step = CHUNK - OVERLAP
total = audio.shape[1]
out = torch.zeros(2, total); wsum = torch.zeros(1, total)
pos = 0; n = 0
with torch.no_grad():
    while pos < total:
        seg = audio[:, pos:pos+CHUNK]
        seg_len = seg.shape[1]
        pad = CHUNK - seg_len
        if pad > 0:
            import numpy as _np
            seg = torch.as_tensor(seg).float()
            seg = torch.nn.functional.pad(seg, (0, pad))
        x = torch.as_tensor(seg).float()
        if x.dim() == 1: x = x.unsqueeze(0)  # (T,)->(1,T)；(2,T) 通道序保留
        x = x.unsqueeze(1) if x.dim() == 2 and x.shape[0] != 2 else x  # (1,T)->(1,1,T)
        if x.shape[0] == 1 and x.dim() == 3: pass  # 已是 (1,1,T)
        x = x.unsqueeze(0) if x.dim() == 2 else x  # (2,T)->(1,2,T)
        est = model(x)  # (1, stems?, 2, T) or (1,2,T)
        if est.dim() == 4: est = est[:, 0]
        # 汉宁窗拼接
        w = torch.hann_window(seg_len, periodic=False).unsqueeze(0) if seg_len==CHUNK else torch.ones(1, seg_len)
        out[:, pos:pos+seg_len] += est[0, :, :seg_len] * w
        wsum[:, pos:pos+seg_len] += w
        pos += step; n += 1
        if n % 10 == 0: print(f"chunk {n}, pos={pos}/{total}", flush=True)
out = out / wsum.clamp(min=1e-8)
# vocal 与 accompaniment：模型输出 karaoke vocal；accomp = input - vocal
vocal = out.numpy().T
accomp = audio.T - vocal
sf.write('/tmp/umx/karaoke_vocal.wav', vocal, SR, subtype='PCM_16')
sf.write('/tmp/umx/karaoke_accomp.wav', accomp, SR, subtype='PCM_16')
print("DONE", vocal.shape, accomp.shape)
