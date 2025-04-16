# EasyOTS
---

## **Безопасная передача паролей, токенов и конфиденциальной информации за пару кликов**
Если вам нужно отправить пароль, API-ключ или другую чувствительную информацию коллеге, клиенту или сервису, что вы будете использовать?\
Почта? Мессенджер? Слишком рискованно!\
EasyOTS — это простое и надёжное решение для одноразового обмена секретами. Отправьте ссылку — и будьте уверены: её можно открыть только один раз.

---
### **Внешний вид приложения**
Создание секрета \
![image](https://github.com/user-attachments/assets/65c62505-32ec-4b1a-b811-837ba52d842e) \
Получение секрета \
![image](https://github.com/user-attachments/assets/1081df05-e0fe-4cf0-924b-8d53221d51dd)

---

Полное описание всех API-методов с учётом добавленного функционала для файлов:

---

### **1. `POST /api/create`**  
**Назначение**: Создание нового секрета (текст или файл)  
**Параметры**:
- `secret` (текст, опционально если есть файл) - текст сообщения
- `ttl` (опционально) - время жизни в секундах (по умолчанию 604800)
- `file` (опционально) - прикрепляемый файл (если `ENABLE_FILE_UPLOAD=true`)

**Работа метода**:
1. Проверяет включена ли загрузка файлов (через `ENABLE_FILE_UPLOAD`)
2. Генерирует 2 UUID:
   - `secretID` - для хранения данных в Redis
   - `pageID` - для публичной ссылки
3. Шифрует данные (AES-256)
4. Сохраняет в Redis:
   ```redis
   SET secret:<secretID> {encrypted_data} EX <ttl>
   SET page:<pageID> <secretID> EX <ttl>
   ```
5. Возвращает JSON с ссылкой:
   ```json
   {
     "status": "success",
     "link": "http://<host>/page/<pageID>",
     "page_id": "<pageID>"
   }
   ```

---

### **2. `GET /api/view/:id`**  
**Назначение**: Получение секрета  
**Параметры**:
- `id` (в URL) - идентификатор страницы (`pageID`)

**Логика**:
1. По `pageID` находит `secretID` в Redis
2. Извлекает данные:
   - Если это файл → возвращает как `application/octet-stream` с заголовком:
     ```
     Content-Disposition: attachment; filename="<original_name>"
     ```
   - Если текст → возвращает JSON:
     ```json
     {"secret": "расшифрованный текст"}
     ```
3. Удаляет данные из Redis после просмотра

---

### **3. `GET /page`**  
**Назначение**: Главная страница  
**Ответ**: HTML с:
- Формой создания секрета
- Полем для файла (если `ENABLE_FILE_UPLOAD=true`)
- Логотипом (если `LOGO_URL` задан)

---

### **4. `GET /page/:id`**  
**Назначение**: Страница просмотра  
**Параметры**:
- `id` (в URL) - `pageID`

**Поведение**:
- Если секрет существует → показывает кнопку "Просмотреть"
- Если просмотрен/не найден → возвращает на главную

---

### **5. `GET /static/*filepath`**  
**Назначение**: Отдача статики (favicon/logo)  
**Особенности**:
- Пути настраиваются через `.env`:
  ```ini
  FAVICON_URL=/static/favicon.ico
  LOGO_URL=/static/logo.png
  ```

---

### **Изменения в API после добавления файлов**:
1. **`POST /api/create`** теперь принимает `multipart/form-data`
2. **`GET /api/view/:id`** может возвращать:
   - Файл (бинарные данные)
   - Текст (JSON)
3. Добавлена проверка размера файла (max 10MB)

---

### **Примеры запросов**:

#### Создание текстового секрета:
```bash
curl -X POST http://localhost:8080/api/create \
  -d "secret=MySecret&ttl=3600"
```

#### Создание с файлом:
```bash
curl -X POST http://localhost:8080/api/create \
  -F "secret=Text" \
  -F "ttl=3600" \
  -F "file=@/path/to/file.pdf"
```

#### Получение секрета:
```bash
curl http://localhost:8080/api/view/<pageID>
```

---

### **Конфигурационные параметры (.env)**:
| Переменная | Описание | Пример |
|------------|----------|--------|
| `ENABLE_FILE_UPLOAD` | Включить загрузку файлов | `true` |
| `ENCRYPTION_KEY` | Ключ шифрования (32 байта) | `0123456789abcdef0123456789abcdef` |
| `FAVICON_URL` | Путь к фавикону | `/static/favicon.ico` |
| `LOGO_URL` | Путь к логотипу | `/static/logo.png` |

---

Новый функционал активируется только при `ENABLE_FILE_UPLOAD=true`.

### **Запуск приложения**:
```bash
# 1.Склонировать проект
# 2.Перейти в каталог проекта
cd <project directory>
# 3.Сборка и запуск
sudo docker-compose up -d --build
```

### Пример настройки внешнего Nginx:
Структура хранения сертификата
```
/etc/nginx/ssl/
├── yourdomain.crt
└── yourdomain.key
```

Пример nginx.conf
```bash
user  nginx;
worker_processes  auto;

error_log  /var/log/nginx/error.log warn;
pid        /var/run/nginx.pid;

events {
    worker_connections  1024;
}

http {
    include       /etc/nginx/mime.types;
    default_type  application/octet-stream;

    log_format  main  '$remote_addr - $remote_user [$time_local] "$request" '
                      '$status $body_bytes_sent "$http_referer" '
                      '"$http_user_agent" "$http_x_forwarded_for"';

    access_log  /var/log/nginx/access.log  main;

    sendfile        on;
    keepalive_timeout  65;

    # Настройки SSL (замените пути)
    ssl_certificate      /etc/nginx/ssl/yourdomain.crt;
    ssl_certificate_key  /etc/nginx/ssl/yourdomain.key;
    ssl_protocols        TLSv1.3;  # Только TLS 1.3
    ssl_ciphers          TLS_AES_256_GCM_SHA384:TLS_CHACHA20_POLY1305_SHA256;
    ssl_prefer_server_ciphers on;
    ssl_session_cache    shared:SSL:10m;
    ssl_session_timeout  10m;
    ssl_ecdh_curve       X25519:secp521r1:secp384r1;

    # Upstream для вашего приложения
    upstream app_server {
        server your_app_server_ip:8080; # Замените на IP приложения
        keepalive 32;
    }

    # HTTP → HTTPS редирект
    server {
        listen 80;
        server_name yourdomain.com www.yourdomain.com;
        
        # Редирект на /page с HTTPS
        return 301 https://$host/page$request_uri;
    }

    # Основной HTTPS сервер
    server {
        listen 443 ssl http2;
        server_name yourdomain.com www.yourdomain.com;

        # Security headers
        add_header Strict-Transport-Security "max-age=31536000; includeSubDomains" always;
        add_header X-Frame-Options DENY;
        add_header X-Content-Type-Options nosniff;
        add_header X-XSS-Protection "1; mode=block";
        add_header Referrer-Policy "strict-origin-when-cross-origin";

        # Лимит загрузки файлов
        client_max_body_size 10M;

        # Корневой редирект на /page
        location = / {
            return 302 /page;
        }

        # Статика
        location /static/ {
            alias /var/www/static/;
            expires 365d;
            access_log off;
            add_header Cache-Control "public";
        }

        # API endpoints
        location /api/ {
            proxy_pass http://app_server;
            proxy_http_version 1.1;
            proxy_set_header Connection "";
            proxy_set_header Host $host;
            proxy_set_header X-Real-IP $remote_addr;
            proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
            proxy_set_header X-Forwarded-Proto $scheme;
            
            proxy_connect_timeout 60s;
            proxy_read_timeout 300s;
            proxy_send_timeout 300s;
        }

        # Все остальные запросы
        location / {
            proxy_pass http://app_server;
            proxy_set_header Host $host;
            proxy_set_header X-Real-IP $remote_addr;
            proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
            proxy_set_header X-Forwarded-Proto $scheme;
        }

        # Блокировка скрытых файлов
        location ~ /\.(?!well-known) {
            deny all;
            access_log off;
            log_not_found off;
        }
    }
}
```


