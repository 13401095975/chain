package com.chain.api;

import com.chain.exception.ChainException;
import com.chain.http.Context;
import com.google.gson.annotations.SerializedName;

import java.util.Map;

public class UnspentOutput {

  public String purpose;

  @SerializedName("transaction_id")
  public String transactionId;

  public int position;

  @SerializedName("asset_id")
  public String assetId;

  @SerializedName("asset_tags")
  public Map<String, Object> assetTags;

  @SerializedName("asset_is_local")
  public String assetIsLocal;

  public long amount;

  @SerializedName("account_id")
  public String accountId;

  @SerializedName("account_tags")
  public Map<String, Object> accountTags;

  @SerializedName("control_program")
  public String controlProgram;

  @SerializedName("reference_data")
  public Map<String, Object> referenceData;

  public static class Items extends PagedItems<UnspentOutput> {
    public Items getPage() throws ChainException {
      Items items = this.context.request("list-unspent-outputs", this.next, Items.class);
      items.setContext(this.context);
      return items;
    }
  }

  public static class QueryBuilder extends BaseQueryBuilder<QueryBuilder> {
    public Items execute(Context ctx) throws ChainException {
      Items items = new Items();
      items.setContext(ctx);
      items.setNext(this.next);
      return items.getPage();
    }

    public QueryBuilder setTimestamp(long time) {
      this.next.timestamp = time;
      return this;
    }
  }
}
