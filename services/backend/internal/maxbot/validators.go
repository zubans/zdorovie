package maxbot

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

var (
	phoneRegex = regexp.MustCompile(`^\+?[78][-\s]?\(?\d{3}\)?[-\s]?\d{3}[-\s]?\d{2}[-\s]?\d{2}$`)
	emailRegex = regexp.MustCompile(`^[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}$`)
	digitsOnly = regexp.MustCompile(`\D`)
)

// ValidateFullName cleans and validates a Russian/Latin name (2..100 characters).
func ValidateFullName(raw string) (string, error) {
	var cleaned strings.Builder
	for _, r := range strings.TrimSpace(raw) {
		if unicode.IsLetter(r) || r == ' ' || r == '-' {
			cleaned.WriteRune(r)
		}
	}
	norm := strings.Join(strings.Fields(cleaned.String()), " ")
	nRunes := len([]rune(norm))
	if nRunes < 2 || nRunes > 100 {
		return "", fmt.Errorf("Пожалуйста, укажите имя и фамилию (от 2 до 100 букв, без цифр и спецсимволов)")
	}
	return norm, nil
}

// NormalizePhoneE164 validates and normalizes Russian/CIS phone numbers to +7XXXXXXXXXX.
func NormalizePhoneE164(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if !phoneRegex.MatchString(trimmed) {
		return "", fmt.Errorf("Пожалуйста, введите номер телефона в формате +7 (999) 000-00-00. Он необходим для связи и оформления чека")
	}
	d := digitsOnly.ReplaceAllString(trimmed, "")
	if len(d) != 11 {
		return "", fmt.Errorf("Пожалуйста, введите номер телефона из 11 цифр в формате +7 (999) 000-00-00")
	}
	if strings.HasPrefix(d, "8") {
		d = "7" + d[1:]
	}
	return "+" + d, nil
}

// ValidateEmail checks strict email format (local@domain.tld, no consecutive dots) and normalizes to lowercase.
func ValidateEmail(raw string) (string, error) {
	trimmed := strings.ToLower(strings.TrimSpace(raw))
	if !emailRegex.MatchString(trimmed) || len(trimmed) > 254 || strings.Contains(trimmed, "..") {
		return "", fmt.Errorf("Некорректный формат e-mail. Укажите действительный адрес, например: name@example.ru — на него придут чек и доступ в кабинет")
	}
	parts := strings.Split(trimmed, "@")
	if len(parts) != 2 || strings.HasPrefix(parts[0], ".") || strings.HasSuffix(parts[0], ".") || strings.HasPrefix(parts[1], ".") || strings.HasPrefix(parts[1], "-") || strings.HasSuffix(parts[1], "-") {
		return "", fmt.Errorf("Некорректный формат e-mail. Проверьте домен и имя почтового ящика (например, name@example.ru)")
	}
	return trimmed, nil
}

// ParsePregnancyStatus validates weeks (1..42) or planning/postpartum status.
func ParsePregnancyStatus(raw string) (string, error) {
	s := strings.ToLower(strings.TrimSpace(raw))
	switch s {
	case "planning", "планирую", "планирую беременность":
		return "Планирую беременность", nil
	case "postpartum", "после родов", "родила":
		return "После родов", nil
	}
	d := digitsOnly.ReplaceAllString(s, "")
	if d == "" {
		return "", fmt.Errorf("Укажите срок в неделях (число от 1 до 42) или нажмите одну из кнопок ниже")
	}
	weeks, err := strconv.Atoi(d)
	if err != nil || weeks < 1 || weeks > 42 {
		return "", fmt.Errorf("Срок беременности должен быть от 1 до 42 недель. Введите число или выберите кнопку")
	}
	return fmt.Sprintf("%d нед.", weeks), nil
}

// EscapeMarkdown escapes MAX Markdown special characters in user-provided text.
func EscapeMarkdown(s string) string {
	replacer := strings.NewReplacer(
		`\`, `\\`,
		`*`, `\*`,
		`_`, `\_`,
		"`", "\\`",
		`[`, `\[`,
		`]`, `\]`,
	)
	return replacer.Replace(s)
}
