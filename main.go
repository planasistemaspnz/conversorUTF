package main

import (
	"bufio"
	"database/sql"
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf16"

	_ "github.com/nakagami/firebirdsql"
)

const (
	defaultPort       = "3050"
	defaultUser       = "SYSDBA"
	defaultPassword   = "masterkey"
	defaultSrcCharset = "WIN1252"
	targetCharset     = "UTF8"
	targetCollation   = "UNICODE_CI_AI"
	targetTable       = "EQUIPAMENTO"
)

type textColumn struct {
	Name          string
	FieldType     int
	CharLen       int
	Nullable      bool
	CharsetName   string
	CollationName string
}

type blobColumn struct {
	Name        string
	CharsetName string
}

type GlobalConfig struct {
	Tipo    int
	IP      string
	Caminho string
}

func main() {
	var flagListar bool
	var flagCorrigirBlobs bool

	flag.BoolVar(&flagListar, "listar", false, "Apenas lista os campos com charset/collation invalido")
	flag.BoolVar(&flagListar, "l", false, "Apenas lista os campos com charset/collation invalido (atalho)")
	flag.BoolVar(&flagCorrigirBlobs, "corrigir-blobs", false, "Corrige apenas os dados dos BLOBs de UTF-16LE para UTF-8")
	flag.BoolVar(&flagCorrigirBlobs, "c", false, "Corrige apenas os dados dos BLOBs de UTF-16LE para UTF-8 (atalho)")
	flag.Parse()

	reader := bufio.NewReader(os.Stdin)
	
	// Determina se a execucao e interativa (rodou sem flags de terminal)
	isInterativo := !flagListar && !flagCorrigirBlobs

	exitCode := 0
	defer func() {
		// Se rodou de forma interativa, sempre solicita ENTER para nao fechar a janela do Windows abruptamente
		if isInterativo {
			fmt.Println()
			fmt.Print("Pressione ENTER para fechar...")
			_, _ = reader.ReadString('\n')
		}
		os.Exit(exitCode)
	}()

	fmt.Println("=== Conversor/Normalizador de Dados Firebird 2.5 ===")
	fmt.Printf("Alvo: Tabela %s -> Normalizacao de BLOBs UTF-16LE para UTF-8\n\n", targetTable)

	exeDir, err := executableDir()
	if err != nil {
		fmt.Printf("Erro ao localizar pasta do executavel: %v\n", err)
		exitCode = 1
		return
	}

	// Localiza o GLOBAL.INI
	iniPath := filepath.Join(exeDir, "GLOBAL.INI")
	var cfg *GlobalConfig

	if err := fileExists(iniPath); err == nil {
		fmt.Printf("Configuracoes carregadas de: %s\n", iniPath)
		cfg, err = parseGlobalINI(iniPath)
		if err != nil {
			fmt.Printf("Erro ao ler GLOBAL.INI: %v\n", err)
			exitCode = 1
			return
		}
	} else {
		// Tenta na pasta pai (ambiente de desenvolvimento)
		parentIniPath := filepath.Join(filepath.Dir(exeDir), "GLOBAL.INI")
		if err2 := fileExists(parentIniPath); err2 == nil {
			fmt.Printf("Configuracoes carregadas de (desenvolvimento): %s\n", parentIniPath)
			cfg, err = parseGlobalINI(parentIniPath)
			if err != nil {
				fmt.Printf("Erro ao ler GLOBAL.INI: %v\n", err)
				exitCode = 1
				return
			}
		} else {
			fmt.Printf("Erro: Arquivo GLOBAL.INI nao encontrado em %s\n", iniPath)
			exitCode = 1
			return
		}
	}

	// Define os dados de conexao com base nas regras do GLOBAL.INI
	dbFile := filepath.Join(cfg.Caminho, "DADOS.GDB")
	host := cfg.IP
	port := defaultPort
	user := defaultUser
	password := defaultPassword
	srcCharset := defaultSrcCharset

	fmt.Printf("Dados de Conexao:\n")
	fmt.Printf("- Arquivo do Banco: %s\n", dbFile)
	fmt.Printf("- Servidor: %s:%s\n\n", host, port)

	if err := fileExists(dbFile); err != nil {
		fmt.Printf("Arquivo de banco nao encontrado: %v\n", err)
		exitCode = 1
		return
	}

	// Fluxo NAO-INTERATIVO (via Terminal com Flags)
	if !isInterativo {
		if flagListar {
			dsn := buildDSN(user, password, host, port, dbFile, srcCharset)
			db, err := sql.Open("firebirdsql", dsn)
			if err != nil {
				fmt.Printf("Erro ao abrir conexao: %v\n", err)
				exitCode = 1
				return
			}
			defer db.Close()

			if err := db.Ping(); err != nil {
				fmt.Printf("Erro ao conectar no banco: %v\n", err)
				exitCode = 1
				return
			}

			fmt.Println("Analisando campos estruturais...")
			if err := listInvalidFields(db); err != nil {
				fmt.Printf("Erro ao listar campos: %v\n", err)
				exitCode = 1
				return
			}
		} else if flagCorrigirBlobs {
			backupPath, err := createBackup(dbFile)
			if err != nil {
				fmt.Printf("Erro ao criar backup: %v\n", err)
				exitCode = 1
				return
			}
			fmt.Printf("Backup criado: %s\n", backupPath)

			dsn := buildDSN(user, password, host, port, dbFile, srcCharset)
			db, err := sql.Open("firebirdsql", dsn)
			if err != nil {
				fmt.Printf("Erro ao abrir conexao: %v\n", err)
				exitCode = 1
				return
			}
			defer db.Close()

			if err := db.Ping(); err != nil {
				fmt.Printf("Erro ao conectar no banco: %v\n", err)
				exitCode = 1
				return
			}

			fmt.Println("Iniciando normalizacao dos BLOBs texto...")
			convertedRows, err := normalizeUTF16BlobData(db, targetTable)
			if err != nil {
				fmt.Printf("Falha ao normalizar dados UTF-16 em BLOB texto: %v\n", err)
				fmt.Println("O backup permanece disponivel para restauracao.")
				exitCode = 1
				return
			}

			fmt.Println("\nCorrecao concluida com sucesso.")
			fmt.Printf("Total de registros BLOB normalizados: %d\n", convertedRows)
		}
		return
	}

	// Fluxo INTERATIVO (via Menu com Loop)
	for {
		fmt.Println("O que deseja fazer?")
		fmt.Println("1 - Listar campos invalidos e detalhar registros BLOB (Antes vs Depois)")
		fmt.Println("2 - Corrigir os dados dos BLOBs (normalizar UTF-16LE para UTF-8)")
		fmt.Println("9 - Sair")
		fmt.Print("Escolha uma opcao (1, 2 ou 9): ")
		opcao := readInput(reader)

		if opcao == "9" {
			fmt.Println("Saindo...")
			break
		}

		if opcao == "1" {
			dsn := buildDSN(user, password, host, port, dbFile, srcCharset)
			db, err := sql.Open("firebirdsql", dsn)
			if err != nil {
				fmt.Printf("Erro ao abrir conexao: %v\n", err)
				fmt.Println()
				continue
			}

			if err := db.Ping(); err != nil {
				fmt.Printf("Erro ao conectar no banco: %v\n", err)
				db.Close()
				fmt.Println()
				continue
			}

			fmt.Println("Analisando campos estruturais...")
			if err := listInvalidFields(db); err != nil {
				fmt.Printf("Erro ao listar campos: %v\n", err)
			}
			db.Close()
			fmt.Println()
			continue
		}

		if opcao == "2" {
			fmt.Print("Deseja realmente corrigir os dados dos BLOBs (UTF-16LE -> UTF-8)? (S/N): ")
			confirm := readInput(reader)
			if !isYes(confirm) {
				fmt.Println("Operacao cancelada.")
				fmt.Println()
				continue
			}

			backupPath, err := createBackup(dbFile)
			if err != nil {
				fmt.Printf("Erro ao criar backup: %v\n", err)
				fmt.Println()
				continue
			}
			fmt.Printf("Backup criado: %s\n", backupPath)

			dsn := buildDSN(user, password, host, port, dbFile, srcCharset)
			db, err := sql.Open("firebirdsql", dsn)
			if err != nil {
				fmt.Printf("Erro ao abrir conexao: %v\n", err)
				fmt.Println()
				continue
			}

			if err := db.Ping(); err != nil {
				fmt.Printf("Erro ao conectar no banco: %v\n", err)
				db.Close()
				fmt.Println()
				continue
			}

			fmt.Println("Iniciando normalizacao dos BLOBs texto...")
			convertedRows, err := normalizeUTF16BlobData(db, targetTable)
			db.Close()
			if err != nil {
				fmt.Printf("Falha ao normalizar dados UTF-16 em BLOB texto: %v\n", err)
				fmt.Println("O backup permanece disponivel para restauracao.")
			} else {
				fmt.Println("\nCorrecao concluida com sucesso.")
				fmt.Printf("Total de registros BLOB normalizados: %d\n", convertedRows)
			}
			fmt.Println()
			continue
		}

		fmt.Println("Opcao invalida. Digite 1, 2 ou 9.")
		fmt.Println()
	}
}

func parseGlobalINI(filePath string) (*GlobalConfig, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var cfg GlobalConfig
	cfg.IP = "127.0.0.1" // valor default padrao

	scanner := bufio.NewScanner(file)
	inDados := false
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, ";") || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section := strings.ToUpper(line[1 : len(line)-1])
			if section == "DADOS" {
				inDados = true
			} else {
				inDados = false
			}
			continue
		}

		if inDados {
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 {
				key := strings.ToUpper(strings.TrimSpace(parts[0]))
				val := strings.TrimSpace(parts[1])
				switch key {
				case "TIPO":
					var t int
					if _, err := fmt.Sscanf(val, "%d", &t); err == nil {
						cfg.Tipo = t
					}
				case "IP":
					cfg.IP = val
				case "CAMINHO":
					cfg.Caminho = val
				}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	if cfg.Caminho == "" {
		return nil, errors.New("chave CAMINHO nao encontrada na secao [DADOS]")
	}

	// Regras do IP baseadas no TIPO:
	// Quando TIPO for = 0 o IP sempre sera 127.0.0.1
	// Quando TIPO for = 1 pega o IP que esta na flag IP
	if cfg.Tipo == 0 {
		cfg.IP = "127.0.0.1"
	}

	return &cfg, nil
}

func listInvalidFields(db *sql.DB) error {
	fmt.Println("\n=== CAMPOS CHAR/VARCHAR COM CHARSET INVALIDO (DIFERENTE DE UTF8/UNICODE_CI_AI) ===")
	cols, err := loadTextColumns(db, targetTable)
	if err != nil {
		return err
	}

	invalidCount := 0
	for _, col := range cols {
		if isCharsetInvalid(col) {
			typeName := "VARCHAR"
			if col.FieldType == 14 {
				typeName = "CHAR"
			}
			fmt.Printf("- Campo: %-20s | Tipo: %-7s(%d) | Charset: %-8s | Collation: %s\n",
				col.Name, typeName, col.CharLen, col.CharsetName, col.CollationName)
			invalidCount++
		}
	}
	if invalidCount == 0 {
		fmt.Println("Todos os campos CHAR/VARCHAR ja estao corretamente definidos como UTF8 / UNICODE_CI_AI.")
	} else {
		fmt.Printf("Total de campos CHAR/VARCHAR invalidos: %d\n", invalidCount)
	}

	fmt.Println("\n=== ANALISE DE CAMPOS BLOB TEXTO (SUB_TYPE 1) ===")
	blobCols, err := loadTextBlobColumnsWithCharset(db, targetTable)
	if err != nil {
		return err
	}

	if len(blobCols) == 0 {
		fmt.Println("Nenhum campo BLOB texto encontrado.")
		return nil
	}

	for _, bcol := range blobCols {
		fmt.Printf("- Campo BLOB: %-15s | Charset Metadado: %s\n", bcol.Name, bcol.CharsetName)
		fmt.Println("  Analisando registros...")
		utf16Count, err := listInvalidBlobRecords(db, targetTable, bcol.Name)
		if err != nil {
			fmt.Printf("  * Erro ao analisar dados do BLOB: %v\n", err)
		} else if utf16Count > 0 {
			fmt.Printf("  * TOTAL: %d registros em %s precisam de normalizacao (detalhados acima).\n\n", utf16Count, bcol.Name)
		} else {
			fmt.Println("  * Dados integros (texto comum ou UTF-8 nativo).\n")
		}
	}

	return nil
}

func listInvalidBlobRecords(db *sql.DB, table, col string) (int, error) {
	query := fmt.Sprintf("SELECT EQU_LANC, %s FROM %s WHERE %s IS NOT NULL ORDER BY EQU_LANC", col, table, col)
	rows, err := db.Query(query)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	count := 0
	for rows.Next() {
		var lanc int
		var raw interface{}
		if err := rows.Scan(&lanc, &raw); err != nil {
			return 0, err
		}
		blobBytes := rawToBytes(raw)
		decText, ok := decodeUTF16LEIfLikely(blobBytes)
		if !ok {
			continue
		}
		count++

		// Formata o "Antes" (cru com pontos)
		rawStr := string(blobBytes)
		cleanBefore := cleanStringForConsole(rawStr)
		if len(cleanBefore) > 60 {
			cleanBefore = cleanBefore[:60] + "..."
		}

		// Formata o "Depois" (decodificado)
		cleanAfter := strings.ReplaceAll(decText, "\n", " ")
		cleanAfter = strings.ReplaceAll(cleanAfter, "\r", "")
		if len(cleanAfter) > 60 {
			cleanAfter = cleanAfter[:60] + "..."
		}

		fmt.Printf("  -> [LANC: %d]\n", lanc)
		fmt.Printf("     * Antes:  %q\n", cleanBefore)
		fmt.Printf("     * Depois: %q\n", cleanAfter)
	}
	return count, rows.Err()
}

func cleanStringForConsole(s string) string {
	var sb strings.Builder
	for _, r := range s {
		if r >= 32 && r <= 126 {
			sb.WriteRune(r)
		} else if r == 0 {
			sb.WriteRune('.')
		} else {
			sb.WriteRune('?')
		}
	}
	return sb.String()
}

func isCharsetInvalid(col textColumn) bool {
	return col.CharsetName != targetCharset || col.CollationName != targetCollation
}

func loadTextColumns(db *sql.DB, table string) ([]textColumn, error) {
	query := `
SELECT
    TRIM(rf.RDB$FIELD_NAME) AS FIELD_NAME,
    f.RDB$FIELD_TYPE,
    COALESCE(f.RDB$FIELD_LENGTH, 0) AS FLEN,
    rf.RDB$NULL_FLAG,
    COALESCE(TRIM(cs.RDB$CHARACTER_SET_NAME), 'NONE') AS CHARSET_NAME,
    COALESCE(TRIM(coll.RDB$COLLATION_NAME), 'NONE') AS COLLATION_NAME
FROM RDB$RELATION_FIELDS rf
JOIN RDB$FIELDS f ON f.RDB$FIELD_NAME = rf.RDB$FIELD_SOURCE
LEFT JOIN RDB$CHARACTER_SETS cs ON f.RDB$CHARACTER_SET_ID = cs.RDB$CHARACTER_SET_ID
LEFT JOIN RDB$COLLATIONS coll ON (f.RDB$COLLATION_ID = coll.RDB$COLLATION_ID AND f.RDB$CHARACTER_SET_ID = coll.RDB$CHARACTER_SET_ID)
WHERE TRIM(rf.RDB$RELATION_NAME) = ?
  AND f.RDB$FIELD_TYPE IN (14, 37)
ORDER BY rf.RDB$FIELD_POSITION`

	rows, err := db.Query(query, strings.ToUpper(table))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var cols []textColumn
	for rows.Next() {
		var c textColumn
		var nullFlag sql.NullInt64
		if err := rows.Scan(&c.Name, &c.FieldType, &c.CharLen, &nullFlag, &c.CharsetName, &c.CollationName); err != nil {
			return nil, err
		}
		c.Nullable = !nullFlag.Valid
		if c.CharLen <= 0 {
			continue
		}
		cols = append(cols, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return cols, nil
}

func loadTextBlobColumns(db *sql.DB, table string) ([]string, error) {
	query := `
SELECT TRIM(rf.RDB$FIELD_NAME)
FROM RDB$RELATION_FIELDS rf
JOIN RDB$FIELDS f ON f.RDB$FIELD_NAME = rf.RDB$FIELD_SOURCE
WHERE TRIM(rf.RDB$RELATION_NAME) = ?
  AND f.RDB$FIELD_TYPE = 261
  AND COALESCE(f.RDB$FIELD_SUB_TYPE, 0) = 1
ORDER BY rf.RDB$FIELD_POSITION`

	rows, err := db.Query(query, strings.ToUpper(table))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var cols []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		cols = append(cols, name)
	}
	return cols, rows.Err()
}

func loadTextBlobColumnsWithCharset(db *sql.DB, table string) ([]blobColumn, error) {
	query := `
SELECT
    TRIM(rf.RDB$FIELD_NAME) AS FIELD_NAME,
    COALESCE(TRIM(cs.RDB$CHARACTER_SET_NAME), 'NONE') AS CHARSET_NAME
FROM RDB$RELATION_FIELDS rf
JOIN RDB$FIELDS f ON f.RDB$FIELD_NAME = rf.RDB$FIELD_SOURCE
LEFT JOIN RDB$CHARACTER_SETS cs ON f.RDB$CHARACTER_SET_ID = cs.RDB$CHARACTER_SET_ID
WHERE TRIM(rf.RDB$RELATION_NAME) = ?
  AND f.RDB$FIELD_TYPE = 261
  AND COALESCE(f.RDB$FIELD_SUB_TYPE, 0) = 1
ORDER BY rf.RDB$FIELD_POSITION`

	rows, err := db.Query(query, strings.ToUpper(table))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var cols []blobColumn
	for rows.Next() {
		var c blobColumn
		if err := rows.Scan(&c.Name, &c.CharsetName); err != nil {
			return nil, err
		}
		cols = append(cols, c)
	}
	return cols, rows.Err()
}

func countUTF16LEBlobs(db *sql.DB, table, col string) (int, error) {
	query := fmt.Sprintf("SELECT %s FROM %s WHERE %s IS NOT NULL", col, table, col)
	rows, err := db.Query(query)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	count := 0
	for rows.Next() {
		var raw interface{}
		if err := rows.Scan(&raw); err != nil {
			return 0, err
		}
		blobBytes := rawToBytes(raw)
		if _, ok := decodeUTF16LEIfLikely(blobBytes); ok {
			count++
		}
	}
	return count, rows.Err()
}

func normalizeOneBlobColumn(db *sql.DB, table, col string) (int, error) {
	tx, err := db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	selectSQL := fmt.Sprintf("SELECT RDB$DB_KEY, %s FROM %s WHERE %s IS NOT NULL", col, table, col)
	rows, err := tx.Query(selectSQL)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	updateSQL := fmt.Sprintf("UPDATE %s SET %s = ? WHERE RDB$DB_KEY = ?", table, col)
	updated := 0
	for rows.Next() {
		var dbKey []byte
		var raw interface{}
		if err := rows.Scan(&dbKey, &raw); err != nil {
			return 0, err
		}

		blobBytes := rawToBytes(raw)
		convertedText, ok := decodeUTF16LEIfLikely(blobBytes)
		if !ok {
			continue
		}

		if _, err := tx.Exec(updateSQL, convertedText, dbKey); err != nil {
			return 0, err
		}
		updated++
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}

	if updated > 0 {
		fmt.Printf("Normalizadas %d linhas na coluna BLOB %s.\n", updated, col)
	}
	return updated, nil
}

func normalizeUTF16BlobData(db *sql.DB, table string) (int, error) {
	blobCols, err := loadTextBlobColumns(db, table)
	if err != nil {
		return 0, err
	}
	if len(blobCols) == 0 {
		return 0, nil
	}

	totalUpdated := 0
	for _, col := range blobCols {
		updated, err := normalizeOneBlobColumn(db, table, col)
		if err != nil {
			return totalUpdated, err
		}
		totalUpdated += updated
	}
	return totalUpdated, nil
}

func rawToBytes(v interface{}) []byte {
	switch x := v.(type) {
	case nil:
		return nil
	case []byte:
		return x
	case string:
		return []byte(x)
	default:
		return []byte(fmt.Sprintf("%v", x))
	}
}

func decodeUTF16LEIfLikely(b []byte) (string, bool) {
	if len(b) < 4 || len(b)%2 != 0 {
		return "", false
	}

	zeroOdd := 0
	pairs := len(b) / 2
	for i := 1; i < len(b); i += 2 {
		if b[i] == 0x00 {
			zeroOdd++
		}
	}
	if zeroOdd*100/pairs < 60 && !(b[0] == 0xFF && b[1] == 0xFE) {
		return "", false
	}

	u16 := make([]uint16, 0, pairs)
	for i := 0; i < len(b); i += 2 {
		u := binary.LittleEndian.Uint16(b[i : i+2])
		if u == 0 {
			continue
		}
		u16 = append(u16, u)
	}
	if len(u16) == 0 {
		return "", false
	}
	return string(utf16.Decode(u16)), true
}

func buildDSN(user, password, host, port, dbFile, charset string) string {
	cleanPath := filepath.Clean(dbFile)
	return fmt.Sprintf("%s:%s@%s:%s/%s?charset=%s", user, password, host, port, cleanPath, charset)
}

func readInput(reader *bufio.Reader) string {
	text, _ := reader.ReadString('\n')
	return strings.TrimSpace(text)
}

func isYes(s string) bool {
	s = strings.TrimSpace(strings.ToLower(s))
	return s == "s" || s == "sim" || s == "y" || s == "yes"
}

func executableDir() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.Dir(exe), nil
}

func fileExists(path string) error {
	st, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return err
		}
		return err
	}
	if st.IsDir() {
		return fmt.Errorf("%s e um diretorio", path)
	}
	return nil
}

func createBackup(dbFile string) (string, error) {
	src, err := os.Open(dbFile)
	if err != nil {
		return "", err
	}
	defer src.Close()

	ext := filepath.Ext(dbFile)
	base := strings.TrimSuffix(dbFile, ext)
	timestamp := time.Now().Format("20060102_150405")
	backup := fmt.Sprintf("%s.backup_%s%s", base, timestamp, ext)

	dst, err := os.Create(backup)
	if err != nil {
		return "", err
	}
	defer dst.Close()

	if _, err := io.Copy(dst, src); err != nil {
		return "", err
	}
	if err := dst.Sync(); err != nil {
		return "", err
	}
	return backup, nil
}
