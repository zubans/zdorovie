using System.Net.Http.Headers;
using System.Text;
using System.Text.Json;

namespace ZdorovieRecorder.Core;

public sealed class RecorderApiClient : IDisposable
{
    private readonly HttpClient _http;
    private readonly string _baseUrl;

    public RecorderApiClient(string baseUrl, string bearerToken)
    {
        _baseUrl = baseUrl.TrimEnd('/');
        _http = new HttpClient { Timeout = TimeSpan.FromMinutes(10) };
        _http.DefaultRequestHeaders.Authorization = new AuthenticationHeaderValue("Bearer", bearerToken.Trim());
    }

    public async Task<List<ConsultationSlotDto>> GetBookedSlotsAsync(CancellationToken ct = default)
    {
        using var resp = await _http.GetAsync($"{_baseUrl}/api/v1/consultations/slots?status=booked", ct);
        resp.EnsureSuccessStatusCode();
        var json = await resp.Content.ReadAsStringAsync(ct);
        var parsed = JsonSerializer.Deserialize<SlotsResponse>(json);
        return parsed?.Slots ?? new List<ConsultationSlotDto>();
    }

    /// <summary>
    /// Uploads consultation WAV/MP3 in 5 MB resumable chunks with retry tolerance for unstable Wi-Fi.
    /// </summary>
    public async Task UploadAudioChunkedAsync(
        long slotId,
        string filePath,
        IProgress<double>? progress = null,
        CancellationToken ct = default)
    {
        if (!File.Exists(filePath))
            throw new FileNotFoundException("Recording file missing", filePath);

        var fileName = Path.GetFileName(filePath);
        var ext = Path.GetExtension(filePath).TrimStart('.').ToLowerInvariant();
        if (string.IsNullOrEmpty(ext)) ext = "wav";

        var initBody = JsonSerializer.Serialize(new
        {
            slot_id = slotId,
            file_name = fileName,
            audio_format = ext,
            source_type = "desktop_recorder"
        });
        using var initReq = new StringContent(initBody, Encoding.UTF8, "application/json");
        using var initResp = await _http.PostAsync($"{_baseUrl}/api/v1/consultations/uploads/init", initReq, ct);
        initResp.EnsureSuccessStatusCode();
        var initJson = await initResp.Content.ReadAsStringAsync(ct);
        var initData = JsonSerializer.Deserialize<ChunkInitResponse>(initJson)
                       ?? throw new InvalidOperationException("Empty upload_id from backend");

        const int chunkSize = 5 * 1024 * 1024;
        var fi = new FileInfo(filePath);
        var totalBytes = Math.Max(1, fi.Length);
        var buffer = new byte[chunkSize];
        var chunkIndex = 0;
        long sentBytes = 0;

        await using (var fs = new FileStream(filePath, FileMode.Open, FileAccess.Read, FileShare.Read))
        {
            while (true)
            {
                var read = await fs.ReadAsync(buffer.AsMemory(0, chunkSize), ct);
                if (read <= 0) break;

                var chunkData = buffer.AsMemory(0, read).ToArray();
                await SendChunkWithRetryAsync(initData.UploadId, chunkIndex, chunkData, ct);

                sentBytes += read;
                chunkIndex++;
                progress?.Report(Math.Min(95.0, (sentBytes * 95.0) / totalBytes));
            }
        }

        using var completeResp = await _http.PostAsync(
            $"{_baseUrl}/api/v1/consultations/uploads/{Uri.EscapeDataString(initData.UploadId)}/complete",
            new StringContent("{}", Encoding.UTF8, "application/json"),
            ct);
        completeResp.EnsureSuccessStatusCode();
        progress?.Report(100.0);
    }

    private async Task SendChunkWithRetryAsync(string uploadId, int index, byte[] data, CancellationToken ct)
    {
        Exception? lastErr = null;
        for (var attempt = 1; attempt <= 4; attempt++)
        {
            try
            {
                using var content = new ByteArrayContent(data);
                content.Headers.ContentType = new MediaTypeHeaderValue("application/octet-stream");
                using var resp = await _http.PutAsync(
                    $"{_baseUrl}/api/v1/consultations/uploads/{Uri.EscapeDataString(uploadId)}/chunk/{index}",
                    content,
                    ct);
                resp.EnsureSuccessStatusCode();
                return;
            }
            catch (Exception ex) when (attempt < 4 && !ct.IsCancellationRequested)
            {
                lastErr = ex;
                await Task.Delay(TimeSpan.FromSeconds(attempt * 2), ct);
            }
        }
        throw lastErr ?? new IOException($"Failed to upload chunk {index}");
    }

    public void Dispose() => _http.Dispose();
}
