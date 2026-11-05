// Money in Steam's currencies: wallet currencies are numbers (1 = USD),
// store prices carry ISO codes ("USD").
const BY_CODE = { 1: "$", 2: "£", 3: "€", 5: "₽", 7: "R$", 8: "¥", 20: "CA$", 21: "A$" };
const BY_ISO = { USD: "$", GBP: "£", EUR: "€", RUB: "₽", BRL: "R$", JPY: "¥", CAD: "CA$", AUD: "A$" };

export function money(cents, currency = 1) {
  const sym = typeof currency === "string" ? (BY_ISO[currency] ?? `${currency} `) : (BY_CODE[currency] ?? "");
  return `${sym}${(cents / 100).toFixed(2)}`;
}

export const CAPS = [0, 100, 200, 500, 1000, 2000, 5000];
