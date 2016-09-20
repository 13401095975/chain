import com.chain.api.Account;
import com.chain.api.Asset;
import com.chain.api.MockHsm;
import com.chain.api.Transaction;
import com.chain.http.Context;
import com.chain.signing.HsmSigner;

import java.math.BigInteger;
import java.net.URL;
import java.util.Arrays;

public class Main {
    public static void main(String[] args) throws Exception {
        Context context = new Context(new URL(System.getenv("CHAIN_API_URL")));
        MockHsm.Key mainKey = MockHsm.Key.create(context);
        HsmSigner.addKey(mainKey);

        new Account.Builder()
                .setAlias("alice")
                .addRootXpub(mainKey.xpub)
                .setQuorum(1)
                .create(context);

        new Account.Builder()
                .setAlias("bob")
                .addRootXpub(mainKey.xpub)
                .setQuorum(1)
                .create(context);

        new Asset.Builder()
                .setAlias("gold")
                .addRootXpub(mainKey.xpub)
                .setQuorum(1)
                .create(context);

        Transaction.Template issuance = new Transaction.Builder()
                .issueByAlias("gold", BigInteger.valueOf(100), null)
                .controlWithAccountByAlias("alice", "gold", BigInteger.valueOf(100), null)
                .build(context);
        Transaction.submit(context, HsmSigner.sign(Arrays.asList(issuance)));

        Transaction.Template spending = new Transaction.Builder()
                .spendFromAccountByAlias("alice", "gold", BigInteger.valueOf(10), null)
                .controlWithAccountByAlias("bob", "gold", BigInteger.valueOf(10), null)
                .build(context);
        Transaction.submit(context, HsmSigner.sign(Arrays.asList(spending)));

        Transaction.Template retirement = new Transaction.Builder()
                .spendFromAccountByAlias("bob", "gold", BigInteger.valueOf(5), null)
                .retireByAlias("gold", BigInteger.valueOf(5), null)
                .build(context);
        Transaction.submit(context, HsmSigner.sign(Arrays.asList(retirement)));
    }
}
