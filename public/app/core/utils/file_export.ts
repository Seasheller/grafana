/* tslint:disable:import-blacklist */
import { isBoolean, isNumber, sortedUniq, sortedIndexOf, unescape as htmlUnescaped } from 'lodash';
import { saveAs } from 'file-saver';
import { isNullOrUndefined } from 'util';
import { dateTime, TimeZone } from '@grafana/data';
import moment from 'moment';

const DEFAULT_DATETIME_FORMAT = 'YYYY-MM-DDTHH:mm:ssZ';
const POINT_TIME_INDEX = 1;
const POINT_VALUE_INDEX = 0;

const END_COLUMN = ',';
const END_ROW = '\r\n';
const QUOTE = '"';
const EXPORT_FILENAME = 'grafana_data_export.csv';

interface SeriesListToCsvColumnsOptions {
  dateTimeFormat: string;
  excel: boolean;
  timezone: TimeZone;
}

const defaultOptions: SeriesListToCsvColumnsOptions = {
  dateTimeFormat: DEFAULT_DATETIME_FORMAT,
  excel: false,
  timezone: '',
};

function csvEscaped(text: string) {
  if (!text) {
    return text;
  }

  return text
    .split(QUOTE)
    .join(QUOTE + QUOTE)
    .replace(/^([-+=@])/, "'$1")
    .replace(/\s+$/, '');
}

const domParser = new DOMParser();
function htmlDecoded(text: string) {
  if (!text) {
    return text;
  }

  const regexp = /&[^;]+;/g;
  function htmlDecoded(value: string) {
    const parsedDom = domParser.parseFromString(value, 'text/html');
    return parsedDom.body.textContent;
  }
  return text.replace(regexp, htmlDecoded).replace(regexp, htmlDecoded);
}

function formatSpecialHeader(useExcelHeader) {
  //  return useExcelHeader ? `sep=${END_COLUMN}${END_ROW}` : '';
  return '';
}

function formatRow(row, addEndRowDelimiter = true) {
  let text = '';
  for (let i = 0; i < row.length; i += 1) {
    if (isBoolean(row[i]) || isNumber(row[i]) || isNullOrUndefined(row[i])) {
      text += row[i];
    } else {
      text += `${QUOTE}${csvEscaped(htmlUnescaped(htmlDecoded(row[i])))}${QUOTE}`;
    }

    if (i < row.length - 1) {
      text += END_COLUMN;
    }
  }
  return addEndRowDelimiter ? text + END_ROW : text;
}

export function convertSeriesListToCsv(seriesList, options: Partial<SeriesListToCsvColumnsOptions>) {
  const { dateTimeFormat, excel } = { ...defaultOptions, ...options };
  //let text = formatSpecialHeader(excel) + formatRow(['Series', 'Time', 'Value']);

  let text = formatSpecialHeader(excel);
  text += formatRow(['导出人', 'admin']);
  text += formatRow(['导出时间', moment(new Date()).format(dateTimeFormat)]);
  text += formatRow(['导出内容']);
  text += formatRow([]);

  text += formatRow(['房间', '点名', '位置', '点位属性', '单位', '时间', '值', '最小值', '最大值', '平均值']);

  for (let seriesIndex = 0; seriesIndex < seriesList.length; seriesIndex += 1) {
    const seriesItemName = ['', '', '', '', ''];
    const seriesItemAlias = seriesList[seriesIndex].alias.split('---');
    const size = seriesItemAlias.length > 5 ? 5 : seriesItemAlias.length;
    for (let num = 0; num < size; num++) {
      seriesItemName[num] = seriesItemAlias[num];
    }

    for (let i = 0; i < seriesList[seriesIndex].datapoints.length; i += 1) {
      const pointTimeStr = moment(seriesList[seriesIndex].datapoints[i][POINT_TIME_INDEX]).format(dateTimeFormat);
      const rowItem = [].concat(seriesItemName, [
        pointTimeStr,
        seriesList[seriesIndex].datapoints[i][POINT_VALUE_INDEX],
      ]);
      if (i === 0) {
        rowItem.push(seriesList[seriesIndex].stats['min']);
        rowItem.push(seriesList[seriesIndex].stats['max']);
        rowItem.push(seriesList[seriesIndex].stats['avg'].toFixed(2));
      }
      text += formatRow(
        rowItem,
        i < seriesList[seriesIndex].datapoints.length - 1 || seriesIndex < seriesList.length - 1
      );
    }
  }
  return text;
}

export function exportSeriesListToCsv(seriesList, options: Partial<SeriesListToCsvColumnsOptions>) {
  const text = convertSeriesListToCsv(seriesList, options);
  saveSaveBlob(text, EXPORT_FILENAME);
}

export function convertSeriesListToCsvColumns(seriesList, options: Partial<SeriesListToCsvColumnsOptions>) {
  const { dateTimeFormat, excel, timezone } = { ...defaultOptions, ...options };
  // add header
  let text =
    formatSpecialHeader(excel) +
    formatRow(
      ['Time'].concat(
        seriesList.map(val => {
          return val.alias;
        })
      )
    );
  // process data
  const extendedDatapointsList = mergeSeriesByTime(seriesList);

  // make text
  for (let i = 0; i < extendedDatapointsList[0].length; i += 1) {
    const timestamp =
      timezone === 'utc'
        ? dateTime(extendedDatapointsList[0][i][POINT_TIME_INDEX])
            .utc()
            .format(dateTimeFormat)
        : dateTime(extendedDatapointsList[0][i][POINT_TIME_INDEX]).format(dateTimeFormat);

    text += formatRow(
      [timestamp].concat(
        extendedDatapointsList.map(datapoints => {
          return datapoints[i][POINT_VALUE_INDEX];
        })
      ),
      i < extendedDatapointsList[0].length - 1
    );
  }

  return text;
}

/**
 * Collect all unique timestamps from series list and use it to fill
 * missing points by null.
 */
function mergeSeriesByTime(seriesList) {
  let timestamps = [];
  for (let i = 0; i < seriesList.length; i++) {
    const seriesPoints = seriesList[i].datapoints;
    for (let j = 0; j < seriesPoints.length; j++) {
      timestamps.push(seriesPoints[j][POINT_TIME_INDEX]);
    }
  }
  timestamps = sortedUniq(timestamps.sort());

  const result = [];
  for (let i = 0; i < seriesList.length; i++) {
    const seriesPoints = seriesList[i].datapoints;
    const seriesTimestamps = seriesPoints.map(p => p[POINT_TIME_INDEX]);
    const extendedDatapoints = [];
    for (let j = 0; j < timestamps.length; j++) {
      const timestamp = timestamps[j];
      const pointIndex = sortedIndexOf(seriesTimestamps, timestamp);
      if (pointIndex !== -1) {
        extendedDatapoints.push(seriesPoints[pointIndex]);
      } else {
        extendedDatapoints.push([null, timestamp]);
      }
    }
    result.push(extendedDatapoints);
  }
  return result;
}

export function exportSeriesListToCsvColumns(seriesList, options: Partial<SeriesListToCsvColumnsOptions>) {
  const text = convertSeriesListToCsvColumns(seriesList, options);
  saveSaveBlob(text, EXPORT_FILENAME);
}

export function convertTableDataToCsv(table, excel = false) {
  let text = formatSpecialHeader(excel);
  // add headline
  text += formatRow(table.columns.map(val => val.title || val.text));
  // process data
  for (let i = 0; i < table.rows.length; i += 1) {
    text += formatRow(table.rows[i], i < table.rows.length - 1);
  }
  return text;
}

export function exportTableDataToCsv(table, excel = false) {
  const text = convertTableDataToCsv(table, excel);
  saveSaveBlob(text, EXPORT_FILENAME);
}

export function saveSaveBlob(payload, fname) {
  const blob = new Blob([payload], { type: 'text/csv;charset=utf-8;header=present;' });
  saveAs(blob, fname);
}
