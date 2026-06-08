# Contexto do Projeto - conversorUTF

Este documento serve como referência de contexto e arquitetura do projeto **conversorUTF** para o assistente **Gemini**. Ele detalha os objetivos, a stack de tecnologia, a estrutura do projeto e o fluxo de funcionamento para facilitar futuras manutenções e extensões.

---

## 1. Visão Geral do Projeto

O **conversorUTF** é um utilitário de linha de comando escrito em **Go** (Golang) cujo principal objetivo é normalizar e reescrever dados textuais dentro de campos **BLOB do tipo texto (sub_type 1)** da tabela `EQUIPAMENTO` em um banco de dados **Firebird 2.5**.

A ferramenta foca na detecção e normalização inteligente de dados armazenados incorretamente em codificação **UTF-16LE** (geralmente gerados por sistemas legados que salvam strings de forma binária), convertendo-os de volta para UTF-8 nativo nos registros correspondentes.

O utilitário não realiza alterações físicas estruturais nos metadados do banco de dados (DDL), atuando puramente na leitura, diagnóstico e normalização direta dos registros de dados corrompidos.

---

## 2. Tecnologias Utilizadas

- **Linguagem:** Go (Golang) - Versão 1.22+
- **Driver do Banco de Dados:** `github.com/nakagami/firebirdsql` (driver nativo de Go para Firebird)
- **Banco de Dados Alvo:** Firebird 2.5 (embora possa funcionar em versões superiores do Firebird)

---

## 3. Estrutura do Repositório

O projeto possui uma estrutura simples e compacta:

```
conversorUTF/
│
├── go.mod                # Declaração do módulo Go e dependências
├── go.sum                # Checksum das dependências do Go
├── main.go               # Arquivo principal com toda a lógica de listagem e normalização de BLOBs
├── build.bat             # Script em lote do Windows para compilar o executável
├── README.md             # Instruções rápidas de uso voltadas ao usuário final
└── gemini.md             # Este arquivo de contexto/memória da IA (gerado automaticamente)
```

---

## 4. Detalhamento do Funcionamento (`main.go`)

### 4.1. Resolução do Banco de Dados (`GLOBAL.INI`)
O utilitário localiza e lê o arquivo `GLOBAL.INI` localizado na pasta do executável (ou na pasta pai se executado em ambiente de desenvolvimento). Os dados de conexão são resolvidos conforme as seguintes regras:
- **IP / Host**: 
  - Se `TIPO=0`, o IP usado sempre será `127.0.0.1`.
  - Se `TIPO=1`, o IP usado será o valor especificado na chave `IP`.
- **Caminho do Banco**: O arquivo do banco sempre será `DADOS.GDB` localizado no diretório indicado em `CAMINHO` (`<CAMINHO>\DADOS.GDB`).
- **Autenticação**: Sempre utiliza as credenciais padrão do Firebird (`SYSDBA` / `masterkey`).

### 4.2. Fluxo de Execução
Ao iniciar, o programa valida a conexão do banco de dados e determina a ação com base no menu interativo ou em flags de console:

1. **Leitura do GLOBAL.INI:** Localiza, parseia e monta a DSN de conexão do Firebird.
2. **Seleção da Ação (Menu / Flags):**
   - **Flags de console:**
     - `-listar` ou `-l`: Executa apenas o **Modo de Listagem**.
     - `-corrigir-blobs` ou `-c`: Executa apenas a **Correção de BLOBs**.
   - **Menu Interativo Loop (sem flags):**
     - O menu executa dentro de um loop de controle (`for`). Após o término de uma ação (1 ou 2), ele exibe o menu novamente na tela. A saída voluntária ocorre pela opção 9.
     - **Opção 1:** Modo de Listagem (diagnóstico). Detalha na tela o ID do registro (`LANC`), a representação crua anterior (`Antes`) e o texto decodificado proposto (`Depois`).
     - **Opção 2:** Correção de BLOBs (corrige dados dos BLOBs sem mexer no DDL).
     - **Opção 9:** Sair do programa.

3. **Modo de Listagem (Diagnóstico):**
   - Identifica colunas textuais (`CHAR`/`VARCHAR`) cujos metadados sejam diferentes de `UTF8`/`UNICODE_CI_AI`.
   - Varre as colunas `BLOB` do tipo texto da tabela `EQUIPAMENTO` buscando registros que contêm dados em codificação binária `UTF-16LE`, imprimindo detalhadamente a lista de registros afetados contendo o ID do Registro (`LANC`), a representação crua anterior (`Antes`) e o texto decodificado proposto (`Depois`).

4. **Correção de BLOBs (Sem Alteração de DDL):**
   - Cria uma cópia física de segurança do arquivo do banco de dados (`.backup_YYYYMMDD_HHMMSS.GDB`).
   - Abre uma transação SQL.
   - Varre os registros dos campos BLOB e normaliza apenas os registros detectados em `UTF-16LE` de volta em UTF-8 nativo.
   - Realiza o **Commit** das alterações em caso de sucesso geral, ou **Rollback** em caso de qualquer erro.

---

## 5. Como Compilar e Rodar

### Compilação (Windows)
Execute o script `build.bat` no terminal para compilar o executável:
```cmd
build.bat
```
Saída: `conversor.exe`.

### Execução em Modo Interativo
Garanta que o `GLOBAL.INI` está na pasta do executável e execute:
```cmd
conversor.exe
```

### Execução por Flags de Linha de Comando
* **Apenas diagnosticar campos/dados:**
  ```cmd
  conversor.exe -listar
  ```
* **Corrigir apenas os dados dos BLOBs (UTF-16LE -> UTF-8):**
  ```cmd
  conversor.exe -corrigir-blobs
  ```

---

## 6. Diretrizes para Modificações Futuras (Para o Assistente)

- **Preservação de Dados (Segurança em Primeiro Lugar):** Nunca remova o processo de backup automático e mantenha a execução das alterações de DML rigorosamente encapsuladas sob transação (`db.Begin()` / `defer tx.Rollback()`).
- **Idempotência:** A normalização de BLOBs detecta se o conteúdo é UTF-16LE de forma heurística, de forma que passar o utilitário múltiplas vezes no mesmo banco de dados é uma operação totalmente segura e idempotente.
- **Tratamento de Codificações:** A conversão de BLOBs utiliza fatias de bytes brutas (`[]byte`). Caso o projeto mude de banco ou precise processar outras tabelas, a tabela alvo e configurações podem ser parametrizadas.
- **Não alteração de DDL:** O utilitário foi configurado para ser puramente de análise e correção de DML de dados (BLOBs), não possuindo comandos que alterem a estrutura estrutural das tabelas (`ALTER TABLE`).
