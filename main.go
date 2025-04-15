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
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

const (
	defaultTTL       = 604800
	redisKeyPrefix   = "secret:"
	pageKeyPrefix    = "page:"
	baseURL          = "http://localhost:8080/page/"
	buttonColorNew   = "#0033A1"
	buttonColorView  = "#00955E"
	placeholderColor = "#ADACAF"
)

var (
	rdb           *redis.Client
	encryptionKey = "0123456789abcdef0123456789abcdef" // 32 ключ для AES-256
)

type Secret struct {
	ID        string `json:"id"`
	PageID    string `json:"page_id"`
	Encrypted string `json:"encrypted"`
	ExpiresAt int64  `json:"expires_at"`
}

type CreateRequest struct {
	Secret string `form:"secret" json:"secret" binding:"required"`
	TTL    int    `form:"ttl" json:"ttl"`
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
}

func init() {
	// проверка ключа на старте
	if len(encryptionKey) != 32 {
		log.Fatal("Encryption key must be exactly 32 bytes long")
	}
}

func initRedis() {
	redisAddr := os.Getenv("REDIS_ADDR")
	if redisAddr == "" {
		redisAddr = "redis:6379"
	}

	rdb = redis.NewClient(&redis.Options{
		Addr:     redisAddr,
		Password: "",
		DB:       0,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := rdb.Ping(ctx).Err(); err != nil {
		log.Fatalf("Failed to connect to Redis: %v", err)
	}
	log.Println("Successfully connected to Redis")
}

func encrypt(plaintext string) (string, error) {
	if plaintext == "" {
		return "", errors.New("empty plaintext provided")
	}

	block, err := aes.NewCipher([]byte(encryptionKey))
	if err != nil {
		return "", fmt.Errorf("failed to create cipher block: %v", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("failed to create GCM: %v", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("failed to generate nonce: %v", err)
	}

	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

func decrypt(ciphertext string) (string, error) {
	data, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", fmt.Errorf("failed to decode base64: %v", err)
	}

	block, err := aes.NewCipher([]byte(encryptionKey))
	if err != nil {
		return "", fmt.Errorf("failed to create cipher block: %v", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("failed to create GCM: %v", err)
	}

	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return "", errors.New("ciphertext too short")
	}

	nonce, ciphertextBytes := data[:nonceSize], data[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertextBytes, nil)
	if err != nil {
		return "", fmt.Errorf("failed to decrypt: %v", err)
	}

	return string(plaintext), nil
}

func createSecret(c *gin.Context) {
	var req CreateRequest

	log.Println("Received create secret request")
	if err := c.ShouldBind(&req); err != nil {
		log.Printf("Bad request: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request format"})
		return
	}

	ttl := req.TTL
	if ttl == 0 {
		ttl = defaultTTL
	}
	log.Printf("Creating secret with TTL: %d seconds", ttl)

	log.Println("Attempting to encrypt secret...")
	encrypted, err := encrypt(req.Secret)
	if err != nil {
		log.Printf("Encryption failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "failed to encrypt secret",
			"details": err.Error(),
		})
		return
	}
	log.Println("Secret encrypted successfully")

	secretID := uuid.New().String()
	pageID := uuid.New().String()

	secret := Secret{
		ID:        secretID,
		PageID:    pageID,
		Encrypted: encrypted,
		ExpiresAt: time.Now().Add(time.Duration(ttl) * time.Second).Unix(),
	}

	secretJSON, err := json.Marshal(secret)
	if err != nil {
		log.Printf("Failed to marshal secret: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to prepare secret for storage"})
		return
	}

	ctx := context.Background()
	if err := rdb.Set(ctx, redisKeyPrefix+secretID, secretJSON, time.Duration(ttl)*time.Second).Err(); err != nil {
		log.Printf("Failed to save secret to Redis: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save secret"})
		return
	}

	if err := rdb.Set(ctx, pageKeyPrefix+pageID, secretID, time.Duration(ttl)*time.Second).Err(); err != nil {
		log.Printf("Failed to save page mapping to Redis: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create secret link"})
		return
	}

	link := baseURL + pageID
	log.Printf("Secret created successfully. Link: %s", link)
	c.JSON(http.StatusOK, gin.H{
		"status":  "success",
		"link":    link,
		"page_id": pageID,
	})
}

func getSecret(c *gin.Context) {
	pageID := c.Param("id")
	if pageID == "" {
		showMainPage(c)
		return
	}

	ctx := context.Background()
	secretID, err := rdb.Get(ctx, pageKeyPrefix+pageID).Result()
	if err == redis.Nil {
		c.HTML(http.StatusOK, "page.html", PageData{
			ButtonText:       "Создать секрет",
			ButtonColor:      buttonColorView,
			ShowForm:         true,
			PlaceholderColor: placeholderColor,
		})
		return
	} else if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get secret ID"})
		return
	}

	// Проверяем существование секрета, но не извлекаем его сразу
	exists, err := rdb.Exists(ctx, redisKeyPrefix+secretID).Result()
	if err != nil || exists == 0 {
		c.HTML(http.StatusOK, "page.html", PageData{
			ButtonText:       "Создать секрет",
			ButtonColor:      buttonColorView,
			ShowForm:         true,
			PlaceholderColor: placeholderColor,
		})
		return
	}

	// Показываем только кнопку для просмотра
	c.HTML(http.StatusOK, "page.html", PageData{
		ButtonText:       "Просмотреть секрет",
		ButtonColor:      buttonColorNew,
		ShowViewButton:   true,
		PageID:           pageID,
		PlaceholderColor: placeholderColor,
	})
}

func viewSecret(c *gin.Context) {
	pageID := c.Param("id")
	if pageID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing page ID"})
		return
	}

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

	// Удаляем секрет после просмотра
	rdb.Del(ctx, redisKeyPrefix+secretID)
	rdb.Del(ctx, pageKeyPrefix+pageID)

	decrypted, err := decrypt(secret.Encrypted)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to decrypt secret"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"secret": decrypted,
	})
}

func showMainPage(c *gin.Context) {
	c.HTML(http.StatusOK, "page.html", gin.H{
		"ButtonText":       "Создать секрет",
		"ButtonColor":      buttonColorView,
		"ShowForm":         true,
		"PlaceholderColor": placeholderColor,
	})
}

func setupRouter() *gin.Engine {
	r := gin.Default()
	r.LoadHTMLGlob("templates/*")

	r.POST("/api/create", createSecret)
	r.GET("/api/view/:id", viewSecret)
	r.GET("/page", showMainPage)
	r.GET("/page/:id", getSecret)

	return r
}

func main() {
	initRedis()
	r := setupRouter()
	log.Println("Starting server on :8080")
	if err := r.Run(":8080"); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}
