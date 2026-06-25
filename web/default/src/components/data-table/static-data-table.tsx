/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import * as React from 'react'
import { cn } from '@/lib/utils'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'

export type StaticTableColumn<TData> = {
  id: string
  header?: React.ReactNode
  cell: (row: TData, index: number) => React.ReactNode
  className?: string
  cellClassName?: string
}

export type StaticDataTableProps<TData> = {
  data: TData[]
  columns: StaticTableColumn<TData>[]
  getRowKey?: (row: TData, index: number) => React.Key
  emptyContent?: React.ReactNode
  emptyClassName?: string
  tableClassName?: string
  className?: string
}

export function StaticDataTable<TData>({
  data,
  columns,
  getRowKey,
  emptyContent,
  emptyClassName,
  tableClassName,
  className,
}: StaticDataTableProps<TData>) {
  return (
    <div className={cn('overflow-hidden rounded-lg border', className)}>
      <Table className={tableClassName}>
        <TableHeader>
          <TableRow>
            {columns.map((column) => (
              <TableHead key={column.id} className={column.className}>
                {column.header}
              </TableHead>
            ))}
          </TableRow>
        </TableHeader>
        <TableBody>
          {data.length === 0 ? (
            <TableRow>
              <TableCell
                colSpan={Math.max(columns.length, 1)}
                className={cn('py-8 text-center text-sm', emptyClassName)}
              >
                {emptyContent ?? '-'}
              </TableCell>
            </TableRow>
          ) : (
            data.map((row, rowIndex) => (
              <TableRow key={getRowKey?.(row, rowIndex) ?? rowIndex}>
                {columns.map((column) => (
                  <TableCell
                    key={column.id}
                    className={column.cellClassName ?? column.className}
                  >
                    {column.cell(row, rowIndex)}
                  </TableCell>
                ))}
              </TableRow>
            ))
          )}
        </TableBody>
      </Table>
    </div>
  )
}

export function BadgeCell({
  className,
  children,
  ...props
}: React.HTMLAttributes<HTMLDivElement>) {
  return (
    <div
      className={cn('inline-flex max-w-full items-center', className)}
      {...props}
    >
      {children}
    </div>
  )
}
