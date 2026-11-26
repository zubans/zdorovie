using System.Text.Json.Serialization;

namespace ZdorovieRecorder.Core;

public sealed class ConsultationSlotDto
{
    [JsonPropertyName("id")]
    public long Id { get; set; }

    [JsonPropertyName("slot_start")]
    public DateTime SlotStart { get; set; }

    [JsonPropertyName("slot_end")]
    public DateTime SlotEnd { get; set; }

    [JsonPropertyName("status")]
    public string Status { get; set; } = "";

    [JsonPropertyName("client_name")]
    public string ClientName { get; set; } = "";

    [JsonPropertyName("client_pregnancy_status")]
    public string ClientPregnancyStatus { get; set; } = "";

    public override string ToString()
    {
        var local = SlotStart.ToLocalTime().ToString("dd.MM.yyyy HH:mm");
        var who = string.IsNullOrWhiteSpace(ClientName) ? "Без имени" : ClientName;
        return $"#{Id} • {local} • {who} ({ClientPregnancyStatus})";
    }
}

public sealed class SlotsResponse
{
    [JsonPropertyName("slots")]
    public List<ConsultationSlotDto> Slots { get; set; } = new();
}

public sealed class ChunkInitResponse
{
    [JsonPropertyName("upload_id")]
    public string UploadId { get; set; } = "";
}

public sealed class LocalRecordingEntry
{
    public string Id { get; set; } = Guid.NewGuid().ToString("N");
    public long SlotId { get; set; }
    public string FilePath { get; set; } = "";
    public DateTime CreatedAtUtc { get; set; } = DateTime.UtcNow;
    public bool Uploaded { get; set; }
    public string LastError { get; set; } = "";
}
