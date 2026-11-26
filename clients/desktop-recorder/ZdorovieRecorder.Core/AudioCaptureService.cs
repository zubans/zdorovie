using NAudio.Wave;
using NAudio.Wave.SampleProviders;

namespace ZdorovieRecorder.Core;

/// <summary>
/// Simultaneously records Windows system loopback audio (Telemost / VK participant voice)
/// and local doctor microphone, mixing both streams into a crash-safe local 16kHz Mono PCM WAV file.
/// </summary>
public sealed class AudioCaptureService : IDisposable
{
    private WasapiLoopbackCapture? _loopback;
    private WaveInEvent? _mic;
    private BufferedWaveProvider? _loopbackBuffer;
    private BufferedWaveProvider? _micBuffer;
    private WaveFileWriter? _writer;
    private CancellationTokenSource? _pumpCts;
    private Task? _pumpTask;

    public bool IsRecording { get; private set; }
    public string? ActiveFilePath { get; private set; }

    public void Start(string outputWavPath)
    {
        if (IsRecording) throw new InvalidOperationException("Capture already running");

        Directory.CreateDirectory(Path.GetDirectoryName(outputWavPath)!);
        ActiveFilePath = outputWavPath;

        var targetFormat = new WaveFormat(16000, 16, 1);
        _writer = new WaveFileWriter(outputWavPath, targetFormat);

        _loopback = new WasapiLoopbackCapture();
        _loopbackBuffer = new BufferedWaveProvider(_loopback.WaveFormat)
        {
            DiscardOnBufferOverflow = true,
            ReadFully = true
        };
        _loopback.DataAvailable += (_, e) => _loopbackBuffer.AddSamples(e.Buffer, 0, e.BytesRecorded);

        var micFormat = new WaveFormat(16000, 16, 1);
        _mic = new WaveInEvent { WaveFormat = micFormat, BufferMilliseconds = 100 };
        _micBuffer = new BufferedWaveProvider(micFormat)
        {
            DiscardOnBufferOverflow = true,
            ReadFully = true
        };
        _mic.DataAvailable += (_, e) => _micBuffer.AddSamples(e.Buffer, 0, e.BytesRecorded);

        var loopSample = new WdlResamplingSampleProvider(
            _loopbackBuffer.ToSampleProvider().ToMono(),
            16000);
        var micSample = _micBuffer.ToSampleProvider();
        var mixer = new MixingSampleProvider(new ISampleProvider[] { loopSample, micSample });
        var pcm16 = mixer.ToWaveProvider16();

        _pumpCts = new CancellationTokenSource();
        var token = _pumpCts.Token;

        _loopback.StartRecording();
        _mic.StartRecording();
        IsRecording = true;

        _pumpTask = Task.Run(async () =>
        {
            var buf = new byte[3200]; // 100ms at 16kHz mono 16-bit
            while (!token.IsCancellationRequested)
            {
                var read = pcm16.Read(buf, 0, buf.Length);
                if (read > 0 && _writer != null)
                {
                    _writer.Write(buf, 0, read);
                    _writer.Flush();
                }
                await Task.Delay(50, token).ConfigureAwait(false);
            }
        }, token);
    }

    public async Task<string?> StopAsync()
    {
        if (!IsRecording) return ActiveFilePath;
        IsRecording = false;

        _pumpCts?.Cancel();
        if (_pumpTask != null)
        {
            try { await _pumpTask.ConfigureAwait(false); } catch (OperationCanceledException) { }
        }

        _loopback?.StopRecording();
        _mic?.StopRecording();
        Dispose();
        return ActiveFilePath;
    }

    public void Dispose()
    {
        _pumpCts?.Cancel();
        _pumpCts?.Dispose();
        _loopback?.Dispose();
        _mic?.Dispose();
        _writer?.Flush();
        _writer?.Dispose();
        _pumpCts = null;
        _loopback = null;
        _mic = null;
        _writer = null;
    }
}
