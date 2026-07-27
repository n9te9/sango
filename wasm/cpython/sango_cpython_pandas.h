#ifndef SANGO_CPYTHON_PANDAS_H
#define SANGO_CPYTHON_PANDAS_H

extern PyObject *PyInit_algos(void);
extern PyObject *PyInit_arrays(void);
extern PyObject *PyInit_byteswap(void);
extern PyObject *PyInit_groupby(void);
extern PyObject *PyInit_hashing(void);
extern PyObject *PyInit_hashtable(void);
extern PyObject *PyInit_index(void);
extern PyObject *PyInit_indexing(void);
extern PyObject *PyInit_internals(void);
extern PyObject *PyInit_interval(void);
extern PyObject *PyInit_join(void);
extern PyObject *PyInit_json(void);
extern PyObject *PyInit_lib(void);
extern PyObject *PyInit_missing(void);
extern PyObject *PyInit_ops_dispatch(void);
extern PyObject *PyInit_ops(void);
extern PyObject *PyInit_pandas_datetime(void);
extern PyObject *PyInit_pandas_parser(void);
extern PyObject *PyInit_parsers(void);
extern PyObject *PyInit_properties(void);
extern PyObject *PyInit_reshape(void);
extern PyObject *PyInit_sas(void);
extern PyObject *PyInit_sparse(void);
extern PyObject *PyInit_testing(void);
extern PyObject *PyInit_tslib(void);
extern PyObject *PyInit_base(void);
extern PyObject *PyInit_ccalendar(void);
extern PyObject *PyInit_conversion(void);
extern PyObject *PyInit_dtypes(void);
extern PyObject *PyInit_fields(void);
extern PyObject *PyInit_nattype(void);
extern PyObject *PyInit_np_datetime(void);
extern PyObject *PyInit_offsets(void);
extern PyObject *PyInit_parsing(void);
extern PyObject *PyInit_period(void);
extern PyObject *PyInit_strptime(void);
extern PyObject *PyInit_timedeltas(void);
extern PyObject *PyInit_timestamps(void);
extern PyObject *PyInit_timezones(void);
extern PyObject *PyInit_tzconversion(void);
extern PyObject *PyInit_vectorized(void);
extern PyObject *PyInit_aggregations(void);
extern PyObject *PyInit_indexers(void);
extern PyObject *PyInit_writers(void);

#define SANGO_PANDAS_INITTAB(name, fn) \
    do { if (PyImport_AppendInittab((name), (fn)) == -1) { return -1; } } while (0)

static int sango_register_pandas(void) {
    SANGO_PANDAS_INITTAB("pandas._libs.algos", PyInit_algos);
    SANGO_PANDAS_INITTAB("pandas._libs.arrays", PyInit_arrays);
    SANGO_PANDAS_INITTAB("pandas._libs.byteswap", PyInit_byteswap);
    SANGO_PANDAS_INITTAB("pandas._libs.groupby", PyInit_groupby);
    SANGO_PANDAS_INITTAB("pandas._libs.hashing", PyInit_hashing);
    SANGO_PANDAS_INITTAB("pandas._libs.hashtable", PyInit_hashtable);
    SANGO_PANDAS_INITTAB("pandas._libs.index", PyInit_index);
    SANGO_PANDAS_INITTAB("pandas._libs.indexing", PyInit_indexing);
    SANGO_PANDAS_INITTAB("pandas._libs.internals", PyInit_internals);
    SANGO_PANDAS_INITTAB("pandas._libs.interval", PyInit_interval);
    SANGO_PANDAS_INITTAB("pandas._libs.join", PyInit_join);
    SANGO_PANDAS_INITTAB("pandas._libs.json", PyInit_json);
    SANGO_PANDAS_INITTAB("pandas._libs.lib", PyInit_lib);
    SANGO_PANDAS_INITTAB("pandas._libs.missing", PyInit_missing);
    SANGO_PANDAS_INITTAB("pandas._libs.ops", PyInit_ops);
    SANGO_PANDAS_INITTAB("pandas._libs.ops_dispatch", PyInit_ops_dispatch);
    SANGO_PANDAS_INITTAB("pandas._libs.pandas_datetime", PyInit_pandas_datetime);
    SANGO_PANDAS_INITTAB("pandas._libs.pandas_parser", PyInit_pandas_parser);
    SANGO_PANDAS_INITTAB("pandas._libs.parsers", PyInit_parsers);
    SANGO_PANDAS_INITTAB("pandas._libs.properties", PyInit_properties);
    SANGO_PANDAS_INITTAB("pandas._libs.reshape", PyInit_reshape);
    SANGO_PANDAS_INITTAB("pandas._libs.sas", PyInit_sas);
    SANGO_PANDAS_INITTAB("pandas._libs.sparse", PyInit_sparse);
    SANGO_PANDAS_INITTAB("pandas._libs.testing", PyInit_testing);
    SANGO_PANDAS_INITTAB("pandas._libs.tslib", PyInit_tslib);
    SANGO_PANDAS_INITTAB("pandas._libs.writers", PyInit_writers);

    SANGO_PANDAS_INITTAB("pandas._libs.tslibs.base", PyInit_base);
    SANGO_PANDAS_INITTAB("pandas._libs.tslibs.ccalendar", PyInit_ccalendar);
    SANGO_PANDAS_INITTAB("pandas._libs.tslibs.conversion", PyInit_conversion);
    SANGO_PANDAS_INITTAB("pandas._libs.tslibs.dtypes", PyInit_dtypes);
    SANGO_PANDAS_INITTAB("pandas._libs.tslibs.fields", PyInit_fields);
    SANGO_PANDAS_INITTAB("pandas._libs.tslibs.nattype", PyInit_nattype);
    SANGO_PANDAS_INITTAB("pandas._libs.tslibs.np_datetime", PyInit_np_datetime);
    SANGO_PANDAS_INITTAB("pandas._libs.tslibs.offsets", PyInit_offsets);
    SANGO_PANDAS_INITTAB("pandas._libs.tslibs.parsing", PyInit_parsing);
    SANGO_PANDAS_INITTAB("pandas._libs.tslibs.period", PyInit_period);
    SANGO_PANDAS_INITTAB("pandas._libs.tslibs.strptime", PyInit_strptime);
    SANGO_PANDAS_INITTAB("pandas._libs.tslibs.timedeltas", PyInit_timedeltas);
    SANGO_PANDAS_INITTAB("pandas._libs.tslibs.timestamps", PyInit_timestamps);
    SANGO_PANDAS_INITTAB("pandas._libs.tslibs.timezones", PyInit_timezones);
    SANGO_PANDAS_INITTAB("pandas._libs.tslibs.tzconversion", PyInit_tzconversion);
    SANGO_PANDAS_INITTAB("pandas._libs.tslibs.vectorized", PyInit_vectorized);

    SANGO_PANDAS_INITTAB("pandas._libs.window.aggregations", PyInit_aggregations);
    SANGO_PANDAS_INITTAB("pandas._libs.window.indexers", PyInit_indexers);

    return 0;
}

#endif