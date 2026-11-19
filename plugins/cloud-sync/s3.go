package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

type s3Client struct {
	endpoint      *url.URL
	region        string
	bucket        string
	accessKey     string
	secretKey     string
	prefix        string
	pathStyle     bool
	allowInsecure bool
	now           func() time.Time
}

func newS3Client(endpoint, region, bucket, accessKey, secretKey, prefix string, pathStyle, allowInsecure bool) (*s3Client, error) {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return nil, errors.New("请填写 S3 Endpoint")
	}
	u, err := url.Parse(endpoint)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" {
		return nil, fmt.Errorf("无效的 S3 Endpoint: %q", endpoint)
	}
	bucket = strings.TrimSpace(bucket)
	if bucket == "" {
		return nil, errors.New("请填写 S3 Bucket")
	}
	accessKey = strings.TrimSpace(accessKey)
	secretKey = strings.TrimSpace(secretKey)
	if accessKey == "" || secretKey == "" {
		return nil, errors.New("请填写 S3 Access Key ID 与 Secret Access Key")
	}
	region = strings.TrimSpace(region)
	if region == "" {
		region = "auto"
	}
	host := u.Hostname()
	if net.ParseIP(host) != nil || strings.EqualFold(host, "localhost") || strings.Trim(u.Path, "/") != "" {
		pathStyle = true
	}
	return &s3Client{
		endpoint:      u,
		region:        region,
		bucket:        bucket,
		accessKey:     accessKey,
		secretKey:     secretKey,
		prefix:        strings.Trim(prefix, "/"),
		pathStyle:     pathStyle,
		allowInsecure: allowInsecure,
		now:           time.Now,
	}, nil
}

func (c *s3Client) Name() string { return "S3" }

func (c *s3Client) Test() error {
	_, err := c.listOnce(c.prefix, "", 1)
	return err
}

func (c *s3Client) List(prefix string) ([]RemoteObject, error) {
	fullPrefix := joinSlash(c.prefix, prefix)
	if fullPrefix != "" {
		fullPrefix += "/"
	}
	var out []RemoteObject
	token := ""
	for {
		res, err := c.listOnce(fullPrefix, token, 1000)
		if err != nil {
			return nil, err
		}
		for _, obj := range res.Contents {
			if strings.HasSuffix(obj.Key, "/") {
				continue
			}
			rel := strings.TrimPrefix(obj.Key, fullPrefix)
			rel = strings.TrimPrefix(rel, "/")
			if rel == "" {
				continue
			}
			var modUnix int64
			if t, err := time.Parse(time.RFC3339Nano, obj.LastModified); err == nil {
				modUnix = t.Unix()
			} else if t, err := time.Parse(time.RFC3339, obj.LastModified); err == nil {
				modUnix = t.Unix()
			}
			out = append(out, RemoteObject{
				Path:        rel,
				Size:        obj.Size,
				ETag:        cleanETag(obj.ETag),
				ModTimeUnix: modUnix,
			})
		}
		if !res.IsTruncated || res.NextContinuationToken == "" {
			break
		}
		token = res.NextContinuationToken
	}
	return out, nil
}

type s3ListBucketResult struct {
	IsTruncated           bool   `xml:"IsTruncated"`
	NextContinuationToken string `xml:"NextContinuationToken"`
	Contents              []struct {
		Key          string `xml:"Key"`
		LastModified string `xml:"LastModified"`
		ETag         string `xml:"ETag"`
		Size         int64  `xml:"Size"`
	} `xml:"Contents"`
}

func (c *s3Client) listOnce(fullPrefix, continuationToken string, maxKeys int) (s3ListBucketResult, error) {
	q := map[string]string{
		"list-type": "2",
		"max-keys":  fmt.Sprintf("%d", maxKeys),
	}
	if fullPrefix != "" {
		q["prefix"] = fullPrefix
	}
	if continuationToken != "" {
		q["continuation-token"] = continuationToken
	}
	resp, err := c.do(http.MethodGet, "", q, nil, nil)
	if err != nil {
		return s3ListBucketResult{}, err
	}
	if resp.Status >= 400 {
		return s3ListBucketResult{}, parseS3Error(resp.Status, resp.Body)
	}
	var parsed s3ListBucketResult
	if err := xml.Unmarshal(resp.Body, &parsed); err != nil {
		return s3ListBucketResult{}, fmt.Errorf("解析 S3 列表响应失败: %w", err)
	}
	return parsed, nil
}

func (c *s3Client) Get(prefix, rel string) ([]byte, RemoteObject, error) {
	key := joinSlash(c.prefix, prefix, rel)
	resp, err := c.do(http.MethodGet, key, nil, nil, nil)
	if err != nil {
		return nil, RemoteObject{}, err
	}
	if resp.Status >= 400 {
		return nil, RemoteObject{}, parseS3Error(resp.Status, resp.Body)
	}
	var modUnix int64
	if lm := headerIgnoreCase(resp.Headers, "Last-Modified"); lm != "" {
		if t, err := http.ParseTime(lm); err == nil {
			modUnix = t.Unix()
		}
	}
	return resp.Body, RemoteObject{
		Path:        rel,
		Size:        int64(len(resp.Body)),
		ETag:        cleanETag(headerIgnoreCase(resp.Headers, "ETag")),
		ModTimeUnix: modUnix,
	}, nil
}

func (c *s3Client) Put(prefix, rel string, data []byte) (RemoteObject, error) {
	key := joinSlash(c.prefix, prefix, rel)
	headers := map[string]string{
		"Content-Type": mimeForPath(rel),
	}
	resp, err := c.do(http.MethodPut, key, nil, headers, data)
	if err != nil {
		return RemoteObject{}, err
	}
	if resp.Status >= 400 {
		return RemoteObject{}, parseS3Error(resp.Status, resp.Body)
	}
	etag := cleanETag(headerIgnoreCase(resp.Headers, "ETag"))
	if etag == "" {
		if headResp, herr := c.do(http.MethodHead, key, nil, nil, nil); herr == nil && headResp.Status < 400 {
			etag = cleanETag(headerIgnoreCase(headResp.Headers, "ETag"))
		}
	}
	return RemoteObject{
		Path:        rel,
		Size:        int64(len(data)),
		ETag:        etag,
		ModTimeUnix: c.now().Unix(),
	}, nil
}

func (c *s3Client) Delete(prefix, rel string) error {
	key := joinSlash(c.prefix, prefix, rel)
	resp, err := c.do(http.MethodDelete, key, nil, nil, nil)
	if err != nil {
		return err
	}
	if resp.Status == http.StatusNotFound {
		return nil
	}
	if resp.Status >= 400 {
		return parseS3Error(resp.Status, resp.Body)
	}
	return nil
}

func (c *s3Client) do(method, objectKey string, query map[string]string, extraHeaders map[string]string, body []byte) (httpResponse, error) {
	basePath := strings.TrimRight(c.endpoint.Path, "/")
	host := c.endpoint.Host
	var rawPath string
	if c.pathStyle {
		rawPath = basePath + "/" + awsURIEncode(c.bucket, true)
	} else {
		host = c.bucket + "." + c.endpoint.Host
		rawPath = basePath
	}
	if objectKey != "" {
		rawPath += "/" + awsURIEncode(objectKey, false)
	}
	if rawPath == "" {
		rawPath = "/"
	}

	canonicalQuery := buildCanonicalQuery(query)
	wireURL := c.endpoint.Scheme + "://" + host + rawPath
	if canonicalQuery != "" {
		wireURL += "?" + canonicalQuery
	}

	now := c.now().UTC()
	amzDate := now.Format("20060102T150405Z")
	dateStamp := now.Format("20060102")
	payloadHash := sha256HexBytes(body)

	headers := map[string]string{
		"Host":                 host,
		"x-amz-date":           amzDate,
		"x-amz-content-sha256": payloadHash,
	}
	for k, v := range extraHeaders {
		headers[k] = v
	}

	canonicalHeaders, signedHeaders := buildCanonicalHeaders(headers)
	canonicalRequest := strings.Join([]string{
		method,
		rawPath,
		canonicalQuery,
		canonicalHeaders,
		signedHeaders,
		payloadHash,
	}, "\n")

	scope := dateStamp + "/" + c.region + "/s3/aws4_request"
	stringToSign := strings.Join([]string{
		"AWS4-HMAC-SHA256",
		amzDate,
		scope,
		sha256HexBytes([]byte(canonicalRequest)),
	}, "\n")

	kDate := hmacSHA256([]byte("AWS4"+c.secretKey), dateStamp)
	kRegion := hmacSHA256(kDate, c.region)
	kService := hmacSHA256(kRegion, "s3")
	kSigning := hmacSHA256(kService, "aws4_request")
	signature := hex.EncodeToString(hmacSHA256(kSigning, stringToSign))

	headers["Authorization"] = fmt.Sprintf(
		"AWS4-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		c.accessKey, scope, signedHeaders, signature,
	)

	return sdkDoHTTP(method, wireURL, headers, body, c.allowInsecure)
}

func buildCanonicalQuery(q map[string]string) string {
	if len(q) == 0 {
		return ""
	}
	type kv struct{ k, v string }
	pairs := make([]kv, 0, len(q))
	for k, v := range q {
		pairs = append(pairs, kv{awsURIEncode(k, true), awsURIEncode(v, true)})
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].k != pairs[j].k {
			return pairs[i].k < pairs[j].k
		}
		return pairs[i].v < pairs[j].v
	})
	parts := make([]string, len(pairs))
	for i, p := range pairs {
		parts[i] = p.k + "=" + p.v
	}
	return strings.Join(parts, "&")
}

func buildCanonicalHeaders(headers map[string]string) (canonical string, signed string) {
	lower := make(map[string]string, len(headers))
	keys := make([]string, 0, len(headers))
	for k, v := range headers {
		lk := strings.ToLower(strings.TrimSpace(k))
		if lk == "" {
			continue
		}
		if _, exists := lower[lk]; !exists {
			keys = append(keys, lk)
		}
		lower[lk] = strings.Join(strings.Fields(strings.TrimSpace(v)), " ")
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		b.WriteString(k)
		b.WriteByte(':')
		b.WriteString(lower[k])
		b.WriteByte('\n')
	}
	return b.String(), strings.Join(keys, ";")
}

func awsURIEncode(s string, encodeSlash bool) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') ||
			c == '_' || c == '-' || c == '~' || c == '.' || (c == '/' && !encodeSlash) {
			b.WriteByte(c)
		} else {
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}

func hmacSHA256(key []byte, data string) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(data))
	return mac.Sum(nil)
}

func sha256HexBytes(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func headerIgnoreCase(h map[string]string, key string) string {
	for k, v := range h {
		if strings.EqualFold(k, key) {
			return v
		}
	}
	return ""
}

type s3ErrorXML struct {
	Code    string `xml:"Code"`
	Message string `xml:"Message"`
}

func parseS3Error(status int, body []byte) error {
	var e s3ErrorXML
	if len(body) > 0 && xml.Unmarshal(body, &e) == nil && (e.Code != "" || e.Message != "") {
		if e.Message != "" {
			return fmt.Errorf("S3 错误 %d (%s): %s", status, e.Code, e.Message)
		}
		return fmt.Errorf("S3 错误 %d (%s)", status, e.Code)
	}
	return fmt.Errorf("S3 HTTP %d", status)
}
