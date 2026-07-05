#include <Arduino.h>

#include <ctype.h>
#include <stdint.h>
#include <string.h>

constexpr uint8_t RI_PIN = 10;
constexpr unsigned long SERIAL_BAUD = 115200;

// Set to true for production builds after the allowlist has been validated.
constexpr bool SAFE_MODE = false;

constexpr size_t MAX_LINE_LENGTH = 96;
constexpr uint8_t MAX_SEQUENCE_CODES = 8;
constexpr uint16_t MAX_DELAY_MS = 10000;

constexpr uint16_t SAFE_CODES[] = {
  0x002, // Volume up
  0x003, // Volume down
  0x004, // Power toggle
  0x005, // Mute toggle
  0x020, // Input 1 / CD role
  0x02F, // Power on / Input 1 role
  0x0D5, // Next input
  0x0D6, // Previous input
  0x0DA, // Power off
  0x170, // Input 2 / Dock role
};

struct Sequence {
  uint16_t delayMs;
  uint16_t codes[MAX_SEQUENCE_CODES];
  uint8_t codeCount;
};

enum class DelayParseResult {
  Ok,
  Bad,
  TooLarge,
};

char lineBuffer[MAX_LINE_LENGTH + 1];
size_t lineLength = 0;
bool discardingLongLine = false;

void handleSerialByte(char value);
void handleLine(char *line);
bool parseSequence(char *line, Sequence *sequence);
char *nextToken(char **cursor);
DelayParseResult parseDelay(const char *token, uint16_t *delayMs);
bool parseCode(const char *token, uint16_t *code);
int hexNibble(char value);
bool isSafeCode(uint16_t code);
void sendRI(uint16_t code);
void sendMark(unsigned int microseconds);
void sendSpace(unsigned int microseconds);
void printOK(const Sequence &sequence);
void printBadCode(const char *token);
void printCode(uint16_t code);
char hexDigit(uint8_t value);

void setup() {
  pinMode(RI_PIN, OUTPUT);
  digitalWrite(RI_PIN, LOW);

  Serial.begin(SERIAL_BAUD);
  delay(2000);

  while (Serial.available() > 0) {
    Serial.read();
  }

  Serial.print(F("READY onkyo-ri seq-v1 safe="));
  Serial.println(SAFE_MODE ? 1 : 0);
}

void loop() {
  while (Serial.available() > 0) {
    int value = Serial.read();
    if (value < 0) {
      return;
    }
    handleSerialByte(static_cast<char>(value));
  }
}

void handleSerialByte(char value) {
  if (value == '\r') {
    return;
  }

  if (value == '\n') {
    if (discardingLongLine) {
      discardingLongLine = false;
      lineLength = 0;
      return;
    }

    lineBuffer[lineLength] = '\0';
    handleLine(lineBuffer);
    lineLength = 0;
    return;
  }

  if (discardingLongLine) {
    return;
  }

  if (lineLength >= MAX_LINE_LENGTH) {
    Serial.println(F("ERR LINE_TOO_LONG"));
    discardingLongLine = true;
    lineLength = 0;
    return;
  }

  lineBuffer[lineLength++] = value;
}

void handleLine(char *line) {
  Sequence sequence;
  if (!parseSequence(line, &sequence)) {
    return;
  }

  for (uint8_t i = 0; i < sequence.codeCount; i++) {
    sendRI(sequence.codes[i]);
    if (i + 1 < sequence.codeCount) {
      delay(sequence.delayMs);
    }
  }

  printOK(sequence);
}

bool parseSequence(char *line, Sequence *sequence) {
  char *cursor = line;
  char *token = nextToken(&cursor);
  if (token == nullptr || strcmp(token, "SEQ") != 0) {
    Serial.println(F("ERR BAD_COMMAND"));
    return false;
  }

  token = nextToken(&cursor);
  DelayParseResult delayResult = parseDelay(token, &sequence->delayMs);
  if (delayResult == DelayParseResult::Bad) {
    Serial.println(F("ERR BAD_DELAY"));
    return false;
  }
  if (delayResult == DelayParseResult::TooLarge) {
    Serial.println(F("ERR DELAY_TOO_LARGE"));
    return false;
  }

  sequence->codeCount = 0;
  while ((token = nextToken(&cursor)) != nullptr) {
    if (sequence->codeCount >= MAX_SEQUENCE_CODES) {
      Serial.println(F("ERR TOO_MANY_CODES"));
      return false;
    }

    uint16_t code = 0;
    if (!parseCode(token, &code)) {
      printBadCode(token);
      return false;
    }

    sequence->codes[sequence->codeCount++] = code;
  }

  if (sequence->codeCount == 0) {
    Serial.println(F("ERR MISSING_CODE"));
    return false;
  }

  if (sequence->delayMs == 0 && sequence->codeCount > 1) {
    Serial.println(F("ERR ZERO_DELAY_MULTI_CODE"));
    return false;
  }

  if (SAFE_MODE) {
    for (uint8_t i = 0; i < sequence->codeCount; i++) {
      if (!isSafeCode(sequence->codes[i])) {
        Serial.print(F("ERR UNSAFE_CODE "));
        printCode(sequence->codes[i]);
        Serial.println();
        return false;
      }
    }
  }

  return true;
}

char *nextToken(char **cursor) {
  char *p = *cursor;
  while (*p != '\0' && isspace(static_cast<unsigned char>(*p))) {
    p++;
  }

  if (*p == '\0') {
    *cursor = p;
    return nullptr;
  }

  char *start = p;
  while (*p != '\0' && !isspace(static_cast<unsigned char>(*p))) {
    p++;
  }

  if (*p != '\0') {
    *p = '\0';
    p++;
  }

  *cursor = p;
  return start;
}

DelayParseResult parseDelay(const char *token, uint16_t *delayMs) {
  if (token == nullptr || *token == '\0') {
    return DelayParseResult::Bad;
  }

  uint32_t value = 0;
  for (const char *p = token; *p != '\0'; p++) {
    if (!isdigit(static_cast<unsigned char>(*p))) {
      return DelayParseResult::Bad;
    }

    value = (value * 10) + static_cast<uint32_t>(*p - '0');
    if (value > MAX_DELAY_MS) {
      return DelayParseResult::TooLarge;
    }
  }

  *delayMs = static_cast<uint16_t>(value);
  return DelayParseResult::Ok;
}

bool parseCode(const char *token, uint16_t *code) {
  if (token == nullptr || token[0] != '0' || (token[1] != 'x' && token[1] != 'X') || token[2] == '\0') {
    return false;
  }

  uint16_t value = 0;
  for (const char *p = token + 2; *p != '\0'; p++) {
    int nibble = hexNibble(*p);
    if (nibble < 0) {
      return false;
    }

    value = static_cast<uint16_t>((value << 4) | static_cast<uint16_t>(nibble));
    if (value > 0x0FFF) {
      return false;
    }
  }

  if (value == 0) {
    return false;
  }

  *code = value;
  return true;
}

int hexNibble(char value) {
  if (value >= '0' && value <= '9') {
    return value - '0';
  }
  if (value >= 'a' && value <= 'f') {
    return value - 'a' + 10;
  }
  if (value >= 'A' && value <= 'F') {
    return value - 'A' + 10;
  }
  return -1;
}

bool isSafeCode(uint16_t code) {
  for (uint8_t i = 0; i < sizeof(SAFE_CODES) / sizeof(SAFE_CODES[0]); i++) {
    if (SAFE_CODES[i] == code) {
      return true;
    }
  }
  return false;
}

void sendRI(uint16_t code) {
  sendMark(3000);
  sendSpace(1000);

  for (int8_t bit = 11; bit >= 0; bit--) {
    sendMark(1000);
    if ((code & (static_cast<uint16_t>(1) << bit)) != 0) {
      sendSpace(2000);
    } else {
      sendSpace(1000);
    }
  }

  sendMark(1000);
  digitalWrite(RI_PIN, LOW);
  delay(20);
}

void sendMark(unsigned int microseconds) {
  digitalWrite(RI_PIN, HIGH);
  delayMicroseconds(microseconds);
}

void sendSpace(unsigned int microseconds) {
  digitalWrite(RI_PIN, LOW);
  delayMicroseconds(microseconds);
}

void printOK(const Sequence &sequence) {
  Serial.print(F("OK SEQ "));
  Serial.print(sequence.delayMs);
  for (uint8_t i = 0; i < sequence.codeCount; i++) {
    Serial.print(' ');
    printCode(sequence.codes[i]);
  }
  Serial.println();
}

void printBadCode(const char *token) {
  Serial.print(F("ERR BAD_CODE"));
  if (token != nullptr && *token != '\0') {
    Serial.print(' ');
    Serial.print(token);
  }
  Serial.println();
}

void printCode(uint16_t code) {
  Serial.print(F("0x"));
  for (int8_t shift = 8; shift >= 0; shift -= 4) {
    Serial.print(hexDigit(static_cast<uint8_t>((code >> shift) & 0x0F)));
  }
}

char hexDigit(uint8_t value) {
  return value < 10 ? static_cast<char>('0' + value) : static_cast<char>('A' + value - 10);
}
