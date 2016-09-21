package com.chain.api;

import com.chain.exception.APIException;
import com.chain.exception.ChainException;
import com.chain.http.Context;
import com.google.gson.annotations.SerializedName;
import com.google.gson.reflect.TypeToken;

import java.lang.reflect.Type;
import java.util.*;

public class Asset {
  public String id;
  public String alias;

  @SerializedName("issuance_program")
  public String issuanceProgram;
  /**
   * The list of keys associated with the asset.
   */
  public Key[] keys;

  /**
   * The number of keys required to sign issuances of the asset
   */
  public int quorum;

  /**
   * The immutable asset definition
   */
  public Map<String, Object> definition;

  /**
   * User-specified tag structure for the asset
   */
  public Map<String, Object> tags;

  /**
   * Specifies whether the asset was defined on the local core, or externally
   */
  public String origin;

  // Error data
  public String code;
  public String message;
  public String detail;

  public static class Key {
    @SerializedName("root_xpub")
    public String rootXpub;

    @SerializedName("asset_pubkey")
    public String assetPubkey;

    @SerializedName("asset_derivation_path")
    public int[] derivationPath;
  }

  public static class Items extends PagedItems<Asset> {
    public Items getPage() throws ChainException {
      Items items = this.context.request("list-assets", this.query, Items.class);
      items.setContext(this.context);
      return items;
    }
  }

  public static class QueryBuilder extends BaseQueryBuilder<QueryBuilder> {
    public Items execute(Context ctx) throws ChainException {
      Items items = new Items();
      items.setContext(ctx);
      items.setQuery(this.query);
      return items.getPage();
    }
  }

  public static class Builder {
    public String alias;
    public Map<String, Object> definition;
    public Map<String, Object> tags;

    @SerializedName("root_xpubs")
    public List<String> rootXpubs;

    public int quorum;

    @SerializedName("client_token")
    private String clientToken;

    public Builder() {
      this.definition = new HashMap<>();
      this.tags = new HashMap<>();
      this.rootXpubs = new ArrayList<>();
    }

    public Asset create(Context ctx) throws ChainException {
      List<Asset> assets = Asset.Builder.create(ctx, Arrays.asList(this));
      Asset result = assets.get(0);
      if (result.id == null) {
        throw new APIException(result.code, result.message, result.detail, null);
      }
      return assets.get(0);
    }

    public static List<Asset> create(Context ctx, List<Builder> assets) throws ChainException {
      for (Builder asset : assets) {
        asset.clientToken = UUID.randomUUID().toString();
      }
      Type type = new TypeToken<List<Asset>>() {}.getType();
      return ctx.request("create-asset", assets, type);
    }

    public Builder setAlias(String alias) {
      this.alias = alias;
      return this;
    }

    public Builder setDefinition(Map<String, Object> definition) {
      this.definition = definition;
      return this;
    }

    public Builder addTag(String key, Object value) {
      this.tags.put(key, value);
      return this;
    }

    public Builder setTags(Map<String, Object> tags) {
      this.tags = tags;
      return this;
    }

    public Builder setQuorum(int quorum) {
      this.quorum = quorum;
      return this;
    }

    public Builder addRootXpub(String xpub) {
      this.rootXpubs.add(xpub);
      return this;
    }

    public Builder setRootXpubs(List<String> xpubs) {
      this.rootXpubs = new ArrayList<>();
      for (String xpub : xpubs) {
        this.rootXpubs.add(xpub);
      }
      return this;
    }
  }
}
