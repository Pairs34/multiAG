# multiAG

Antigravity IDE'nin agent isteklerini kendi [9Router](https://github.com/decolua/9router) sunucunuza yönlendiren, eklenti gerektirmeyen deneysel köprü. Yerel web panelinden bağlantıyı görebilir, model seçebilir ve doğrudan Google yönlendirmesine geçebilirsiniz.

[English README](README.md) · [Kurulum ve kurtarma](docs/setup.md) · [Mimari ve sınırlar](docs/architecture.md)

IDE'de seçtiğiniz **model** router'a gönderilir. İsteği hangi hesabın karşılayacağını 9Router havuzu belirler. IDE'deki Google profilini değiştirmek, router'da aynı hesabı seçmez. Hesap ekleme ve rotasyon 9Router panelinden yönetilir.

## Kurulum

Go 1.23+, Python 3.10+, Antigravity IDE ve çalışan bir 9Router API anahtarı gerekir. IDE'yi bir kez açın; kullanıcı `settings.json` dosyasının mevcut olduğundan emin olun.

Linux / macOS:

```sh
go build -trimpath -buildvcs=false -o bin/agrouter ./cmd/agrouter
python3 scripts/router_trial.py setup --router https://router.example.com --wire-format openai
```

Windows PowerShell:

```powershell
go build -trimpath -buildvcs=false -o bin/agrouter.exe ./cmd/agrouter
python scripts/router_trial.py setup --router https://router.example.com --wire-format openai
```

Örnek adresi kendi router adresinizle değiştirin; sonuna `/v1` eklemeyin. Anahtar gizli olarak sorulur. İlk kurulumdan sonra IDE penceresini yeniden yükleyin. Ayar dosyası yanlış algılanırsa `--settings "dosya/yolu/settings.json"` kullanın.

Paneli açmak için:

```sh
python3 scripts/router_trial.py panel
```

Windows'ta `python3` yerine `python` kullanın. Model alanını boş bırakınca IDE seçimi izlenir. Paneldeki değişiklikler sonraki isteklere uygulanır; IDE'nin yeniden başlatılması gerekmez.

Linux'ta kullanıcı oturumuyla otomatik başlatma:

```sh
python3 scripts/install_router_autostart.py
```

Google'a dönüş:

```sh
python3 scripts/router_trial.py stop
```

Ardından IDE penceresini yeniden yükleyin. Windows/macOS'ta köprü işlemini ayrıca kapatın. Ayrıntılar [kurulum belgesinde](docs/setup.md).

## Destek durumu

Gerçek Linux IDE oturumunda metin akışı ve araç çağrısı doğrulandı. Windows/macOS için çapraz derleme kontrolü yapılır; bu platformlarda gerçek IDE entegrasyonu henüz doğrulanmadı. Kullanılan IDE ayarı belgelenmemiştir ve güncellemelerle değişebilir.

Agent mesajları, ilgili proje bağlamı ve araç sonuçları router'a gider. IDE oturum bilgileri, kota sorguları ve tab tamamlama Google tarafında kalır. IDE'de görünen kota, router havuzunun toplam kotası değildir. Desteklenmeyen medya/araç biçimleri ve diğer sınırlar [mimari belgesindedir](docs/architecture.md).

Public kaynak paketi oluşturmak için `python3 scripts/package_source.py` çalıştırın. Anahtarlar, kişisel ayarlar ve çalışma verileri pakete dahil edilmez. MIT lisanslı bağımsız topluluk projesidir; Google ile bağlantısı yoktur.
