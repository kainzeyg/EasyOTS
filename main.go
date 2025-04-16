package main

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

const (
	defaultTTL       = 604800 // 7 days in seconds
	redisKeyPrefix   = "secret:"
	pageKeyPrefix    = "page:"
	maxFileSize      = 10 << 20 // 10MB
	buttonColorNew   = "#0033A1"
	buttonColorView  = "#00955E"
	placeholderColor = "#ADACAF"
)

type Config struct {
	EnableFileUpload bool
	EncryptionKey    string
	FaviconURL       string
	LogoURL          string
	SiteTitle        string
}

var (
	rdb       *redis.Client
	appConfig Config
)

type Secret struct {
	ID        string `json:"id"`
	PageID    string `json:"page_id"`
	Encrypted string `json:"encrypted"`
	FileName  string `json:"file_name,omitempty"`
	FileData  []byte `json:"file_data,omitempty"`
	ExpiresAt int64  `json:"expires_at"`
}

type PageData struct {
	ButtonText       string
	ButtonColor      string
	ShowSecret       bool
	SecretValue      string
	ShowForm         bool
	ShowViewButton   bool
	PageID           string
	PlaceholderColor string
	EnableFileUpload bool
	FaviconURL       string
	LogoURL          string
	SiteTitle        string
}

func init() {
	// Загрузка конфигурации
	appConfig = Config{
		EnableFileUpload: os.Getenv("ENABLE_FILE_UPLOAD") == "true",
		EncryptionKey:    os.Getenv("ENCRYPTION_KEY"),
		FaviconURL:       os.Getenv("FAVICON_URL"),
		LogoURL:          os.Getenv("LOGO_URL"),
		SiteTitle:        os.Getenv("SITE_TITLE"),
	}

	// Проверка обязательных переменных
	if appConfig.EncryptionKey == "" {
		log.Fatal("ENCRYPTION_KEY must be set in .env")
	}

	// Дополнительные проверки
	if len(appConfig.EncryptionKey) != 32 {
		log.Fatal("ENCRYPTION_KEY must be 32 bytes long")
	}
}

func initRedis() {
	rdb = redis.NewClient(&redis.Options{
		Addr:     "redis:6379",
		Password: "",
		DB:       0,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if _, err := rdb.Ping(ctx).Result(); err != nil {
		log.Fatalf("Failed to connect to Redis: %v", err)
	}
}

func encrypt(data string) (string, error) {
	block, err := aes.NewCipher([]byte(appConfig.EncryptionKey))
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}

	ciphertext := gcm.Seal(nonce, nonce, []byte(data), nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

func decrypt(ciphertext string) (string, error) {
	data, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", err
	}

	block, err := aes.NewCipher([]byte(appConfig.EncryptionKey))
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return "", errors.New("ciphertext too short")
	}

	nonce, ciphertextBytes := data[:nonceSize], data[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertextBytes, nil)
	if err != nil {
		return "", err
	}

	return string(plaintext), nil
}

func createSecret(c *gin.Context) {
	// Parse form data
	if err := c.Request.ParseMultipartForm(maxFileSize); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to parse form"})
		return
	}

	secretText := c.PostForm("secret")
	ttl, _ := strconv.Atoi(c.PostForm("ttl"))
	if ttl == 0 {
		ttl = defaultTTL
	}

	var fileName string
	var fileData []byte

	if appConfig.EnableFileUpload {
		file, header, err := c.Request.FormFile("file")
		if err == nil {
			defer file.Close()
			fileName = header.Filename
			fileData, err = io.ReadAll(file)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "file read error"})
				return
			}
		}
	}

	encrypted, err := encrypt(secretText)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to encrypt secret"})
		return
	}

	secret := Secret{
		ID:        uuid.New().String(),
		PageID:    uuid.New().String(),
		Encrypted: encrypted,
		FileName:  fileName,
		FileData:  fileData,
		ExpiresAt: time.Now().Add(time.Duration(ttl) * time.Second).Unix(),
	}

	secretJSON, err := json.Marshal(secret)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to marshal secret"})
		return
	}

	ctx := context.Background()
	if err := rdb.Set(ctx, redisKeyPrefix+secret.ID, secretJSON, time.Duration(ttl)*time.Second).Err(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save secret"})
		return
	}

	if err := rdb.Set(ctx, pageKeyPrefix+secret.PageID, secret.ID, time.Duration(ttl)*time.Second).Err(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create secret link"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":  "success",
		"link":    "http://" + c.Request.Host + "/page/" + secret.PageID,
		"page_id": secret.PageID,
	})
}

func viewSecret(c *gin.Context) {
	pageID := c.Param("id")
	ctx := context.Background()

	secretID, err := rdb.Get(ctx, pageKeyPrefix+pageID).Result()
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "secret not found"})
		return
	}

	secretJSON, err := rdb.Get(ctx, redisKeyPrefix+secretID).Result()
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "secret not found"})
		return
	}

	var secret Secret
	if err := json.Unmarshal([]byte(secretJSON), &secret); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to process secret"})
		return
	}

	// Delete after viewing
	rdb.Del(ctx, redisKeyPrefix+secret.ID)
	rdb.Del(ctx, pageKeyPrefix+pageID)

	if secret.FileName != "" {
		c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s", secret.FileName))
		c.Data(http.StatusOK, "application/octet-stream", secret.FileData)
		return
	}

	decrypted, err := decrypt(secret.Encrypted)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to decrypt secret"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"secret": decrypted})
}

func getSecretPage(c *gin.Context) {
	pageID := c.Param("id")
	if pageID == "" {
		showMainPage(c)
		return
	}

	ctx := context.Background()
	secretID, err := rdb.Get(ctx, pageKeyPrefix+pageID).Result()
	if err != nil {
		showMainPage(c)
		return
	}

	exists, err := rdb.Exists(ctx, redisKeyPrefix+secretID).Result()
	if err != nil || exists == 0 {
		showMainPage(c)
		return
	}

	c.HTML(http.StatusOK, "page.html", PageData{
		ButtonText:       "Просмотреть секрет",
		ButtonColor:      buttonColorNew,
		ShowViewButton:   true,
		PageID:           pageID,
		EnableFileUpload: appConfig.EnableFileUpload,
		FaviconURL:       appConfig.FaviconURL,
		LogoURL:          appConfig.LogoURL,
		SiteTitle:        appConfig.SiteTitle,
	})
}

func showMainPage(c *gin.Context) {
	c.HTML(http.StatusOK, "page.html", PageData{
		ButtonText:       "Создать секрет",
		ButtonColor:      buttonColorView,
		ShowForm:         true,
		EnableFileUpload: appConfig.EnableFileUpload,
		FaviconURL:       appConfig.FaviconURL,
		LogoURL:          appConfig.LogoURL,
		SiteTitle:        appConfig.SiteTitle,
	})
}

func setupRouter() *gin.Engine {
	r := gin.Default()
	r.LoadHTMLGlob("templates/*")
	r.Static("/static", "./static")

	r.POST("/api/create", createSecret)
	r.GET("/api/view/:id", viewSecret)
	r.GET("/page", showMainPage)
	r.GET("/page/:id", getSecretPage)

	return r
}

func main() {
	initRedis()
	r := setupRouter()
	log.Println("Server started on :8080")
	log.Fatal(r.Run(":8080"))
}
