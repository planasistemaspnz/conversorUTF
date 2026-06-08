# Conversor Firebird 2.5 para UTF8 (tabela EQUIPAMENTO)

Este utilitario em Go:

- pergunta se deseja iniciar a conversao;
- cria backup automatico do arquivo `.GDB`;
- converte todos os campos `CHAR`/`VARCHAR` da tabela `EQUIPAMENTO` para `CHARACTER SET UTF8 COLLATE UNICODE_CI_AI`;
- faz tudo em transacao (rollback em caso de erro).

## Requisitos

- Go 1.22+
- Firebird 2.5 acessivel por `host:porta`
- permissao para ler/escrever no arquivo do banco

## Build do EXE

No Windows, dentro desta pasta:

```bat
build.bat
```

Saida esperada: `conversor.exe`.

## Execucao

```bat
conversor.exe
```

O programa solicita:

- confirmacao `S/N`;
- caminho do banco (padrao: `DADOS - Copia (3).GDB` no mesmo diretorio do `.exe`);
- host, porta, usuario, senha;
- charset atual dos dados (padrao `WIN1252`).

## Observacoes importantes

- Se o charset atual real dos dados nao for `WIN1252`, informe corretamente na execucao para evitar transliteracao incorreta.
- O alvo foi definido como `UTF8` + `UNICODE_CI_AI`. Se quiser outra collation, altere as constantes `targetCharset` e `targetCollation` no `main.go`.
