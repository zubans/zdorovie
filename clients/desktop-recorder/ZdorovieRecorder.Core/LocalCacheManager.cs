using System.Text.Json;

namespace ZdorovieRecorder.Core;

/// <summary>
/// Persists recording metadata under %APPDATA%\ZdorovieRecorder\cache so sudden crashes or network drops
/// never lose consultation audio and can always resume uploading.
/// </summary>
public sealed class LocalCacheManager
{
    public string CacheDir { get; }
    private readonly string _indexPath;

    public LocalCacheManager()
    {
        var appData = Environment.GetFolderPath(Environment.SpecialFolder.ApplicationData);
        CacheDir = Path.Combine(appData, "ZdorovieRecorder", "cache");
        Directory.CreateDirectory(CacheDir);
        _indexPath = Path.Combine(CacheDir, "recordings.json");
    }

    public string AllocateRecordingPath(long slotId)
    {
        var stamp = DateTime.UtcNow.ToString("yyyyMMdd_HHmmss");
        return Path.Combine(CacheDir, $"slot_{slotId}_{stamp}.wav");
    }

    public List<LocalRecordingEntry> LoadAll()
    {
        if (!File.Exists(_indexPath)) return new List<LocalRecordingEntry>();
        try
        {
            var raw = File.ReadAllText(_indexPath);
            return JsonSerializer.Deserialize<List<LocalRecordingEntry>>(raw) ?? new List<LocalRecordingEntry>();
        }
        catch
        {
            return new List<LocalRecordingEntry>();
        }
    }

    public void SaveEntry(LocalRecordingEntry entry)
    {
        var list = LoadAll();
        var idx = list.FindIndex(e => e.Id == entry.Id);
        if (idx >= 0) list[idx] = entry;
        else list.Add(entry);
        File.WriteAllText(_indexPath, JsonSerializer.Serialize(list, new JsonSerializerOptions { WriteIndented = true }));
    }

    public List<LocalRecordingEntry> PendingUploads() =>
        LoadAll().Where(e => !e.Uploaded && File.Exists(e.FilePath)).ToList();
}
