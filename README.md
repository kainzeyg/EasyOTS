# EasyOTS

Вот полное описание всех API-методов с учётом добавленного функционала для файлов:

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

### Запуск приложения:
```bash
# 1.Склонировать проект
# 2.Перейти в каталог проекта
cd <project directory>
# 3.Сборка и запуск
sudo docker-compose up -d --build
```
