declare module "sql.js" {
  export interface SqlJsStatement {
    bind(params?: unknown): boolean;
    step(): boolean;
    get(): unknown[];
    getAsObject(): Record<string, unknown>;
    getColumnNames(): string[];
    free(): void;
  }
  export interface SqlJsDatabase {
    exec(sql: string): unknown;
    run(sql: string, params?: unknown): unknown;
    prepare(sql: string, params?: unknown): SqlJsStatement;
    export(): Uint8Array;
    getRowsModified(): number;
    close(): void;
  }
  export interface SqlJsStatic {
    Database: new (data?: Uint8Array | null) => SqlJsDatabase;
  }
  export type BindParams = unknown;
  export default function initSqlJs(config?: Record<string, unknown>): Promise<SqlJsStatic>;
}

declare module "prop-types" {
  interface Requireable<T = unknown> {
    (props: unknown, propName: string, componentName: string, ...rest: unknown[]): Error | null;
    isRequired: Requireable<T>;
  }
  const PropTypes: {
    any: Requireable;
    string: Requireable<string>;
    number: Requireable<number>;
    bool: Requireable<boolean>;
    func: Requireable<(...args: never[]) => unknown>;
    symbol: Requireable<symbol>;
    array: Requireable<unknown[]>;
    object: Requireable<Record<string, unknown>>;
    node: Requireable<unknown>;
    element: Requireable<unknown>;
    elementType: Requireable<unknown>;
    shape: (spec: Record<string, unknown>) => Requireable<Record<string, unknown>>;
    arrayOf: (type: Requireable) => Requireable<unknown[]>;
    oneOf: (values: unknown[]) => Requireable;
    oneOfType: (types: Requireable[]) => Requireable;
    instanceOf: (ctor: new (...args: never[]) => unknown) => Requireable;
    exact: (spec: Record<string, unknown>) => Requireable<Record<string, unknown>>;
  };
  export default PropTypes;
}