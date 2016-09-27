package com.chain.api;

import com.google.gson.annotations.SerializedName;

import com.chain.exception.ChainException;
import com.chain.http.Context;

import java.util.ArrayList;

public abstract class BaseQueryBuilder<T extends BaseQueryBuilder<T>> {
  protected Query next;

  public abstract <S extends PagedItems> S execute(Context ctx) throws ChainException;

  public BaseQueryBuilder() {
    this.next = new Query();
  }

  public T useIndexById(String id) {
    this.next.indexId = id;
    return (T) this;
  }

  public T useIndexByAlias(String alias) {
    this.next.indexAlias = alias;
    return (T) this;
  }

  public T setAfter(String after) {
    this.next.after = after;
    return (T) this;
  }

  public T setFilter(String filter) {
    this.next.filter = filter;
    return (T) this;
  }

  public T addFilterParameter(String param) {
    this.next.filterParams.add(param);
    return (T) this;
  }

  public T setFilterParameters(ArrayList<String> params) {
    this.next.filterParams = new ArrayList<>();
    for (String p : params) {
      this.next.filterParams.add(p);
    }
    return (T) this;
  }
}
