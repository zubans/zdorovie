/**
 * Interactive input mask "+7 (***) ***-**-**" for Russian/CIS phone numbers.
 * Keeps backspace friendly over fixed punctuation characters.
 */
export function formatPhoneMask(rawValue, prevValue = '') {
  let digits = String(rawValue || '').replace(/\D/g, '')
  if (!digits) return ''

  const prevDigits = String(prevValue || '').replace(/\D/g, '')
  const isDeleting = Boolean(prevValue) && String(rawValue).length < String(prevValue).length

  // If user pressed Backspace over literal ")", "-" or space, drop the last national digit too
  if (isDeleting && digits === prevDigits) {
    digits = digits.slice(0, -1)
  }
  if (!digits) return ''

  // Strip country prefix 7 or 8
  let national = digits
  if (digits.startsWith('7') || digits.startsWith('8')) {
    national = digits.slice(1)
  }
  national = national.slice(0, 10)

  if (national.length === 0) {
    return isDeleting ? '' : '+7 ('
  }

  let formatted = `+7 (${national.slice(0, 3)}`
  if (national.length >= 4) {
    formatted += `) ${national.slice(3, 6)}`
  }
  if (national.length >= 7) {
    formatted += `-${national.slice(6, 8)}`
  }
  if (national.length >= 9) {
    formatted += `-${national.slice(8, 10)}`
  }
  return formatted
}

export function extractPhoneDigits(value) {
  let digits = String(value || '').replace(/\D/g, '')
  if (digits.length === 11 && (digits.startsWith('7') || digits.startsWith('8'))) {
    digits = digits.slice(1)
  } else if (digits.startsWith('7') || digits.startsWith('8')) {
    digits = digits.slice(1)
  }
  return digits.slice(0, 10)
}

export function isPhoneComplete(value) {
  return extractPhoneDigits(value).length === 10
}

/**
 * Validates e-mail against a strict format mask (local@domain.tld).
 */
export function validateEmailMask(raw) {
  const email = String(raw || '').trim()
  if (!email) {
    return { valid: false, reason: 'Введите E-mail' }
  }
  if (email.length > 254 || /\s/.test(email)) {
    return { valid: false, reason: 'E-mail не должен содержать пробелов' }
  }
  const atIdx = email.indexOf('@')
  if (atIdx <= 0 || atIdx !== email.lastIndexOf('@')) {
    return { valid: false, reason: 'Укажите E-mail в формате name@domain.ru' }
  }
  const local = email.slice(0, atIdx)
  const domain = email.slice(atIdx + 1)

  if (local.length > 64 || local.startsWith('.') || local.endsWith('.') || local.includes('..')) {
    return { valid: false, reason: 'Некорректная часть до символа @ (проверьте точки)' }
  }
  if (!/^[a-zA-Z0-9.!#$%&'*+/=?^_`{|}~-]+$/.test(local)) {
    return { valid: false, reason: 'Недопустимые символы в имени адреса' }
  }
  if (domain.includes('..') || !domain.includes('.')) {
    return { valid: false, reason: 'Укажите домен с зоной, например mail.ru или yandex.ru' }
  }
  const parts = domain.split('.')
  const tld = parts[parts.length - 1]
  if (!/^[a-zA-Z]{2,}$/.test(tld)) {
    return { valid: false, reason: 'Доменная зона (.ru, .com и др.) должна содержать минимум 2 буквы' }
  }
  for (const label of parts) {
    if (!/^[a-zA-Z0-9](?:[a-zA-Z0-9-]*[a-zA-Z0-9])?$/.test(label)) {
      return { valid: false, reason: 'Некорректное доменное имя' }
    }
  }
  return { valid: true, reason: '' }
}
