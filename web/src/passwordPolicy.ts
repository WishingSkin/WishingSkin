export function validNewPassword(value: string) {
  const length = Array.from(value).length
  return length >= 8 && length <= 128 && /[A-Za-z]/.test(value) && /[0-9]/.test(value) && /[\p{P}\p{S}]/u.test(value)
}
