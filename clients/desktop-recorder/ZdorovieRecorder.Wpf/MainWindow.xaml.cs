using System.Windows;
using System.Windows.Threading;
using ZdorovieRecorder.Core;

namespace ZdorovieRecorder.Wpf;

public partial class MainWindow : Window
{
    private readonly LocalCacheManager _cache = new();
    private readonly AudioCaptureService _capture = new();
    private readonly DispatcherTimer _timer = new() { Interval = TimeSpan.FromSeconds(1) };

    private DateTime _recordStartedUtc;
    private LocalRecordingEntry? _activeEntry;

    public MainWindow()
    {
        InitializeComponent();
        _timer.Tick += (_, _) =>
        {
            var elapsed = DateTime.UtcNow - _recordStartedUtc;
            TimerText.Text = $"{(int)elapsed.TotalMinutes:D2}:{elapsed.Seconds:D2}";
        };
        RefreshPendingList();
    }

    private RecorderApiClient CreateClient()
    {
        var url = ServerUrlBox.Text.Trim();
        var token = TokenBox.Password.Trim();
        if (string.IsNullOrEmpty(url) || string.IsNullOrEmpty(token))
            throw new InvalidOperationException("Укажите URL сервера и API-токен врача.");
        return new RecorderApiClient(url, token);
    }

    private async void LoadSlots_Click(object sender, RoutedEventArgs e)
    {
        try
        {
            StatusText.Text = "Загрузка расписания консультаций...";
            using var api = CreateClient();
            var slots = await api.GetBookedSlotsAsync();
            SlotsCombo.ItemsSource = slots;
            if (slots.Count > 0) SlotsCombo.SelectedIndex = 0;
            StatusText.Text = $"Загружено записей: {slots.Count}";
        }
        catch (Exception ex)
        {
            StatusText.Text = $"Ошибка загрузки слотов: {ex.Message}";
        }
    }

    private async void ToggleRecord_Click(object sender, RoutedEventArgs e)
    {
        if (!_capture.IsRecording)
        {
            if (SlotsCombo.SelectedItem is not ConsultationSlotDto slot)
            {
                MessageBox.Show("Сначала выберите слот консультации из списка.", "Внимание");
                return;
            }
            var path = _cache.AllocateRecordingPath(slot.Id);
            _activeEntry = new LocalRecordingEntry { SlotId = slot.Id, FilePath = path };
            _cache.SaveEntry(_activeEntry);

            _capture.Start(path);
            _recordStartedUtc = DateTime.UtcNow;
            _timer.Start();
            RecordBtn.Content = "⏹ Остановить и отправить врачу/ИИ";
            StatusText.Text = $"Идёт запись (WASAPI Loopback + микрофон) -> {System.IO.Path.GetFileName(path)}";
            return;
        }

        _timer.Stop();
        RecordBtn.IsEnabled = false;
        RecordBtn.Content = "Обработка и отправка...";
        await _capture.StopAsync();
        RecordBtn.Content = "⏺ Начать запись консультации";
        RecordBtn.IsEnabled = true;

        if (_activeEntry != null)
        {
            await UploadEntryAsync(_activeEntry);
        }
    }

    private async Task UploadEntryAsync(LocalRecordingEntry entry)
    {
        try
        {
            using var api = CreateClient();
            StatusText.Text = $"Загрузка файла {System.IO.Path.GetFileName(entry.FilePath)}...";
            var progress = new Progress<double>(pct => UploadBar.Value = pct);
            await api.UploadAudioChunkedAsync(entry.SlotId, entry.FilePath, progress);
            entry.Uploaded = true;
            entry.LastError = "";
            _cache.SaveEntry(entry);
            StatusText.Text = "Аудиозапись доставлена и поставлена в очередь на расшифровку Whisper!";
        }
        catch (Exception ex)
        {
            entry.LastError = ex.Message;
            _cache.SaveEntry(entry);
            StatusText.Text = $"Файл сохранён локально, ошибка отправки: {ex.Message}";
        }
        RefreshPendingList();
    }

    private async void RetryPending_Click(object sender, RoutedEventArgs e)
    {
        foreach (var entry in _cache.PendingUploads())
        {
            await UploadEntryAsync(entry);
        }
    }

    private void RefreshPendingList()
    {
        PendingList.ItemsSource = _cache.PendingUploads()
            .Select(p => $"Слот #{p.SlotId} • {System.IO.Path.GetFileName(p.FilePath)} • {(string.IsNullOrEmpty(p.LastError) ? "Ожидает" : p.LastError)}")
            .ToList();
    }
}
