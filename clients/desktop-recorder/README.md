# Zdorovie Desktop Recorder (Windows / .NET 8 WPF)

Утилита для врача: перехватывает системный звук видеозвонка (**Яндекс Телемост** / **VK Звонки** через **WASAPI Loopback**) одновременно с голосом врача с микрофона, сохраняет поток в локальный отказоустойчивый буфер `%APPDATA%\ZdorovieRecorder\cache` и отправляет запись по кускам (`5 МБ`) в платформу для Whisper-транскрибации и LLM-суммаризации.

## Сборка под Windows (.NET 8 SDK)

```powershell
cd clients\desktop-recorder
dotnet restore
dotnet build -c Release
dotnet publish ZdorovieRecorder.Wpf\ZdorovieRecorder.Wpf.csproj -c Release -r win-x64 --self-contained -o publish\win-x64
```

## Использование

1. Введите адрес платформы (например, `https://zdorovie.example.ru` или `http://localhost:8880`).
2. Введите персональный `DOCTOR_API_TOKEN` и нажмите **«Загрузить слоты»**.
3. Выберите активный слот консультации и нажмите **«Начать запись консультации»**.
4. По окончании встречи нажмите **«Остановить и отправить»** — файл отправится в `POST /api/v1/consultations/uploads/init` -> `chunk/{i}` -> `complete`.
