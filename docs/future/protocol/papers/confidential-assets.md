# Confidential Assets

## Introduction

A blockchain works because all participants can validate every transaction. In order to do this, those transactions must be broadcast to the entire network. Confidential Assets is an evolution of the Chain Protocol that enables hiding amounts and their asset types while enabling the network to verify the integrity of the blockchain.

Confidential Assets is a design based on original scheme Confidential Transactions (CT) by Adam Back and Gregory Maxwell. The motivation for Chain’s Confidential Assets scheme is to hide not only the amounts in a transaction (as in CT), but also the asset types. In contrast to networks like Bitcoin that only have one asset circulating, and thus do not have to worry about this problem, Chain-enabled networks typically contain many different asset types, such as several different currencies and securities. We do not want other network participants to know which assets are being traded, or in what volume, but still want them to be able to verify that the transactions are valid. This is the problem Confidential Assets solves.

In addition, the scheme enables both confidential and non-confidential assets to co-exist on a single blockchain and allows for selective disclosure of private data to designated third parties. It makes privacy a native feature of Chain’s data model and architecture, not an add-on or special out-of-band case. It is compatible with blockchain programs and relies on established cryptographic primitives that allow us to optimize for performance and scalability.

## Confidential values

TBD: mapping asset ids to points
TBD: mapping amounts to multiples of asset points
TBD: blinding amounts
TBD: perfect binding vs perfect hiding
TBD: balancing amounts
TBD: blinding asset ids
TBD: balancing multi-asset transactions

## Range proofs

TBD: range-proving amounts
TBD: range-proving asset ids


## Confidential issuance

TBD: why and how

## Excess commitments

TBD: why and how

## Disclosure proofs

TBD: prove a specific asset id / amount


## Security analysis

### Theorem D1: risk of denial of service via asset ID point hashing is negligible

Since the amount of computations it takes to compute an [Asset ID point](ca.md#asset-id-point) is variable,
malicious prover may find an `assetID` by brute force that would require the verifier
to perform arbitrary amount of point decoding operations. The following theorem states
that the risk is negligible as the malicious prover has to perform exponentially more work than 
the verifier being attacked.

**Theorem D1:** In order to make the verifier perform `N` point decodings
when mapping asset ID to a point, the expected number of decodings to be
performed by the prover enumerating arbitrary asset IDs is `2^N`.

**Proof:** The output of a hash function used by [Asset ID Point](ca.md#asset-id-point) 
algorithm is a random 256-bit string, where the first 255 bits encode the y-coordinate and 
the last bit encodes the lowest bit of the x-coordinate. Decoding procedure extracts the y-coordinate
as is, verifies that it is below 2^255 + 19 and recovers the matching x-coordinate. If the procedure fails,
hashing and decoding is retried with an incremented counter. 

The failure can happen in one of 3 cases:

1. If the y-coordinate is >= than 2^255 - 19.
2. If the recovered x-coordinate is zero and the lowest bit of x is 1.
3. If there is no square root for a given y-coordinate.

We will consider probabilities of failing checks #1 and #2 as negligible:

1. There are only 19 in 2^255 invalid y-coordinates to fail the check #1. 
2. There are only 2 in (2^255 - 19) valid y-coordinates that fail the check #2 when the x-coordinate has non-zero lowest bit.

Check #3 fails with probability 0.5 because only half of elements in a prime field are valid square roots.

As a result, the probability of choosing an asset ID to cause N decoding failures in a row after M tries follows the binomial distribution. For probability 0.5, M equals 2^N.∎

**Discussion:** The alternative solution is to have creators of asset identifiers to choose an issuance
program (that defines the asset ID) so that their asset ID always hashes to a valid point.
Unfortunately, this approach rejects roughly half of existing asset IDs created on
the blockchains deployed before the extension to _Confidential Assets_. 
We consider that to preserve asset ID compatibility at the risk of an infeasible 
denial of service attack is an acceptable tradeoff.

### Theorem A1: asset commitment is perfectly binding

**Theorem A1:** Asset ID commitment is perfectly binding for asset ID under the assumption that underlying hash functions are first and second preimage-resistant.

**Proof sketch:**

1. Non-blinded asset ID commitment is perfectly binding.
2. One-item range proof perfectly binds the blinded commitment to the same asset ID as a previous commitment.
3. Multi-item range proof binds to asset ID in at least one of the previous commitments.
4. Multi-item range proof binds to a unique asset ID.
5. By induction, the sequence of (re)blinded commitments are perfectly binding to the original asset ID.

**Proof:** 

**1.** First, we observe that [PointHash](ca.md#pointhash) function that maps asset ID to an [asset ID point](ca.md#asset-id-point) based on Keccak permutation is a perfectly binding commitment under the assumption that the underlying Keccak instance is second preimage-resistant (that is, probability of finding a different asset ID mapping to the same _asset ID point_ is negligible). The non-blinded commitment is simply a pair of an _asset ID point_ with a [point at infinity](ca.md#zero-point).

**2.** Next, an [Asset Range Proof](ca.md#asset-range-proof) that consists of one item proves that the given commitment perfectly commits to the same value as the previous asset commitment:

1. Let `H1,B1` be the previous asset commitment, known to commit to a certain asset ID `a`:

        H1 = a + b·G
        B1 = b·J
        
2. The asset range proof for `H2,B2` aims to prove the following for an unknown blinding factor `x`:
    
        H2 = H1 + x·G
        B2 = B1 + x·J

    In other words, the `H2-H1` and `B2-B1` must have the same discrete log in respect to `G` and `J` respectively.
3. The verification procedure is:
    1. Receive scalars `e`, `s`, and points `H1`, `H2`, `B1`, `B2`.
    2. Compute `R1 = s·G - e·(H2 - H1)`.
    3. Compute `R2 = s·J - e·(B2 - B1)`.
    4. Compute `e' = Hash(R1||R2||H1||B1||H2||B2)`.
    5. Verify `e' == e`.
4. Lets use notation `A/B` to mean discrete log of `A` in respect to `B`.
5. Lets factor out `s` from definitions of R1 and R2:

        s = R1/G + e·(H2 - H1)/G
        s = R2/J + e·(B2 - B1)/J

6. The above equality must hold for any value of `e` because it is determined after `R1`, `R2`, `H1`, `H2`, `B1`, `B2` and due to preimage resistance of the hash function cannot be predicted before these points are fixed. Therefore, `R1/G` must equal `R2/J` and `(H2 - H1)/G` must equal `(B2 - B1)/J`, therefore both `H2` and `B2` are proven to blind the `H1` and `B2` by the same secret factor `x`.

**3.** When [Asset Range Proof](ca.md#asset-range-proof) contains more than one item in its ring signature, it is easy to see that each signature element could be seen as a binding signature with the rest of the ring acting as a Fiat-Shamir challenge:

1. One-item ring signature uses the following hash function (note that challenge `e` is both inside and outside the hash function that uses discrete log P/G as a trapdoor):

        e = Hash(s·G - e·P)

2. Two-item ring signature, has a slightly more complex hash function, but the principle is the same. The same ring signature can be seen as one of two possible Sigma-protocols:

        (a) e1 = Hash(s0·G - Hash(s1·G - e1·P1)·P0)
        (b) e0 = Hash(s1·G - Hash(s0·G - e0·P0)·P1)

3. In the example (a) above, the `s1` can be computed if discrete log P1/G is known to satisfy challenge `e1`, while the remaining value `s0` can be chosen freely (“forged”). Likewise for (b), but the s2has to be computed to satisfy challenge `e0`.
4. Similarly for more items: due to symmetry, any one s-value must be computed, while the remaining s-values can be forged.

**4.** The above proves that at least one signature element is correctly computed, but does not prove that it is the only one. Indeed, in general case it is possible to create a ring signature over arbitrary public keys with several or even all s-values being computed using the corresponding private keys.

In our case, a valid asset range proof proves equality of discrete logs for both halves of the difference between the target commitment and the previous commitment. Below we are demonstrating that it is not possible to have a pair of such discrete logs opening the target commitment to two different previous commitments:

1. Let `(H,B)` be the target asset ID commitment and `(Hi,Bi)` the i-th previous asset commitment, where:

        Hi = c_i·G + Ai
        Bi = c_i·J

2. Let’s assume that `(H,B)` is both a reblinded commitment to `(H1,B1)` and to `(H2,B2)`:

        (H,B) == (H1 + x·G, B1 + x·J)
        (H,B) == (H2 + y·G, B2 + y·J)

3. The equality must hold for each half of the commitment independently:

             (c1 + x)·J == (c2 + y)·J
        A1 + (c1 + x)·G == A2 + (c2 + y)·G

4. From the equations above it follows, that both commitments must necessarily commit to the same asset ID `A1 == A2`.

**5.** Every confidential asset ID commitment starts with a non-confidential commitment (either at point of [issuance](ca.md#issuance-asset-range-proof) or [migration](txvm.md#migrate)) which is perfectly binding according to **(1)**. Since every re-blinded commitment and associated range proof maintain the binding, by induction, any subsequent asset ID commitment is perfectly binding to a correctly issued/upgraded asset ID.



### Theorem A2: asset commitment is computationally hiding

Sketch: 

Given H1,B1,H2,B2 determine if H2 is a blinded H1 with the same factor as B2 in respect to B1.
In this proof we assume absence of the signature that proves the binding.

Let X = x*G = H2 - H1
Let Y = y*J = B2 - B1

Testing whether x == y requires either breaking ECDLP or DDH:

ECDLP: 

    If j is known (such that J = j*G), then (j^-1)*Y = y*G and can be compared with X.

DDH:

    If we can decide whether Y is DH of (X,J), then we can prove link between H1 and H2:

    DH(x*G, j*G) =?= x*j*G

    E.g. with pairings:

    e(X,J) =?= e(G,Y)

Neither ECDLP nor DDH are tractable for Ed25519.







### Asset Commitment (AC)

    A = PointHash(assetid)

Non-blinded asset commitment:

    (A, O)

Blinded asset commitment:

    (A + c*G, c*J)

### Issuance Asset Range Proof (IARP) - WIP

    M = rand    - unique marker point

    x - blinding factor
    y - blinding factor

    H = A + x*G - blinded commitment
    B = x*J     - blinding commitment
    T = y*M     - tracing point
    Y = y*G     - issuance key

Verifier:

    Need to prove knowledge of `y` and `x`.

    1. Receive e,sx,sy,M,H,B,Bm,T,Y
    2. Compute R1 = sx*G - e*(H-A)
    3. Compute R2 = sx*J - e*B
    4. Compute R3 = sy*M - e*T
    5. Compute R4 = sy*G - e*Y
    6. Compute e' = Hash(R1||R2||R3||R4)
    7. Verify e' == e

Signer:

    1. Choose kx = random
    2. Choose ky = random
    3. Compute R1 = kx*G
    4. Compute R2 = kx*J
    5. Compute R3 = ky*M
    6. Compute R4 = ky*G
    7. Compute e = Hash(R1||R2||R3||R4)
    8. Compute sx = kx + e*x
    9. Compute sy = ky + e*y
    10. Return (e,sx,sy)

Proof of soundness:

    let R_i = r_i*G
    let J = j*G
    let M = m*G
    let T = t*G
    etc.

    Need to prove:

        1. b == (h - a)*j
        2. t == y*m
    
    1. Factor out `sx` from R1, R2, R3, R4 definitions:

        sx = r1 + e*(h - a)
        j*sx = r2 + e*b

    2. Factor out `sy` from R1, R2, R3, R4 definition:

        sy = r4 + e*y
        m*sy = r3 + e*t

    3. Equality must hold for any value of `e` (since it's determined after all vars except sy/sx).
       Therefore (placing definition of sx and sy from the first equation to the second equation):

        m*y == t
        j*(h-a) == b

        QED.

Proof of issuance:

    The proof above proves simultaneously the knowledge of `y` and commitment to a blinding factor for asset `A`.
    Provided Y is associated with A, this makes sure that the holder of `y` is blinding the `A`.

Proof of tracing:

    The binding property guarantees that T is `y*M`, meaning a multiplication of a public marker point
    is done by the issuance private key (same key as in `Y = y*G`).

Proof of safety of issuance:

    Proof of issuance cannot be replayed as all proofs of knowledge are tied to a given transaction.

Proof of safety of tracing:

    Tracing point cannot be replayed since proofs are tied to a given transaction.
    Two tracing points from different transactions cannot be linked since points M are unique and tied to a transaction.


Proof of blinding:

    1. To link tracing point T to an issuance key Y, one needs to break either ECDLP or DDH:

        ECDLP: extract all the dlogs and check if `t == m*y`
        DDH:   verify e(Y,M) == e(G,T) (using pairing e(xG,yG) -> e(xyG,G))

    2. To link tracing point to other tracing points `T' = y*M'`, one must break ECDLP to DDH
       since the points M are unique for each transaction:

        ECDLP: extract all the dlogs and check if `t == m*y && t' == m'*y`
        DDH:   verify e(Y,M) == e(G,T) && e(Y,M') == e(G,T')

    3. To link A to (B,H) one also needs to break either ECDLP or DDH.

        ECDLP: extract all dlogs, and check if j*(h-a) == b
        DDH:   verify e(H-A,J) == e(G,B)

Ring version:

    1. Iterate (A,Y) pairs.
    2. Compute a chain of e0 -> e1 -> ... e0'
    3. Verify e0' == e0

    At least one `{R_i}` tuple in the ring will have to be defined before its 
    factor `e` is determined, allowing application of the proof of soundness.

    Since the ring is perfectly symmetrical, the proof of blinding is reduced 
    to the set of the elements in the ring, without revealing to which element
    the commitment is bound to.



### Asset Range Proof (ARP)

Verifier:

    Needs to verify:

       H2 = H1 + x*G
       B2 = B1 + x*J

    1. Receive e, s, H1, H2, B1, B2
    2. Compute R1 = s*G - e*(H2 - H1)
    3. Compute R2 = s*J - e*(B2 - B1)
    4. Compute e' = Hash(R1||R2)
    5. Verify e' == e

Signer:

    1. Choose k = random
    2. Compute R1 = k*G
    3. Compute R2 = k*J
    4. Compute e = Hash(R1||R2)
    5. Compute s = k + e*x
    6. Return (e,s)

Proof of soundness:

    let R1 = r1*G
    let R2 = r2*G
    let J  = j*G
    let H1 = h1*G
    let H2 = h2*G
    let B1 = b1*J
    let B2 = b2*J

    Need to prove that (x can be any value):

       b2 == b1 + x*j
       h2 == h1 + x

       In other words: 
       b2 - b1 == (h2 - h2)*j

    1. Factor out `s` from R1 and R2 definitions:
     
        s = r1 + e*(h2 - h1)
        j*s = r2 + e*(b2 - b1)
    
    2. Equality must hold for any value of `e` (since it's determined after r1,r2,h1,h2,b1,b2).
       Therefore:

        j*(h2-h1) == b2 - b1

       which is what we are looking for.

Proof of blinding:

    Given H1,B1,H2,B2 determine if H2 is a blinded H1 with the same factor as B2 in respect to B1.
    In this proof we assume absence of the signature that proves the binding.

    Let X = x*G = H2 - H1
    Let Y = y*J = B2 - B1

    Testing whether x == y requires either breaking ECDLP or DDH:

    ECDLP: 
    
        If j is known (such that J = j*G), then (j^-1)*Y = y*G and can be compared with X.

    DDH:
    
        If we can decide whether Y is DH of (X,J), then we can prove link between H1 and H2:

        DH(x*G, j*G) =?= x*j*G

        E.g. with pairings:

        e(X,J) =?= e(G,Y)

    Neither ECDLP nor DDH are tractable for Ed25519.

Ring version:

    1. Iterate (H1,B1) pairs.
    2. Compute a chain of e0 -> e1 -> ... e0'
    3. Verify e0' == e0

    At least one `{R_i}` tuple in the ring will have to be defined before its 
    factor `e` is determined, allowing application of the proof of soundness.

    Since the ring is perfectly symmetrical, the proof of blinding is reduced 
    to the set of the elements in the ring, without revealing to which element
    the commitment is bound to.


### Value Commitment (VC)


Non-blinded value commitment:

    (v*H, B)

Blinded value commitment:

    (v*H + f*G, v*B + f*J)


### Value Range Proof

Given:

    Asset commitment:
        H = a*G + c*G
        B = c*J

    Value commitment:
        V = v*H + f*G
        D = v*B + f*J

Need to verify:

    (V,D) commit to value `v` using asset commitment (H,B)

Verifier:

    Needs to verify (f could be anything, v chosen by verifier):

       V = v*H + f*G
       D = v*B + f*J

    1. Receive e, s, v, H, B, V, D
    2. Compute R1 = s*G - e*(V - v*H)
    3. Compute R2 = s*J - e*(D - v*B)
    4. Compute e' = Hash(R1||R2)
    5. Verify e' == e

Signer:

    1. Choose k = random
    2. Compute R1 = k*G
    3. Compute R2 = k*J
    4. Compute e = Hash(R1||R2)
    5. Compute s = k + e*f
    6. Return (e,s)

Proof of balance:

    Provided:
        Sum(V_i) == 0
        Sum(D_i) == 0
    
    Need:
        Sum(v_j)*A == 0 for each A

    1. Sum(D_i) == Sum[(v_i*c_i + f_i)*j*G] == 0
    2. Sum(V_i) == Sum[(v_i*a_i + v_i*c_i + f_i)*G] == 0
    3. Therefore: Sum[v_i*a_i] == 0
    4. What would be the probability of having v1*a1 == v2*a2 provided a1 != a2, and v1,v2 in 62-bit range?
    5. We assume a1 is given, v1,v2 could be tweaked to match a2. 
    6. The space of v1/v2 is 2^124 combinations.
    7. Both a1 and a2 are proven to be carried over since issuance where a1,a2 are 
       pseudo-randomly generated from Hash(assetid) with space 2^252.
    8. Therefore, chance that a2/a1 falls into one of possible 2^124 states for v1/v2 is:
    9. P = 2^124 / 2^252 = 2^128.
    10. Meaning, attacker is expected to perform 2^128 hashing operations to find a2 that allows morphing to/from a1.
    11. 






## Acknowledgements

TBD.

In this article we examine the current state of blockchain privacy and introduce Chain’s confidentiality work, called _Confidential Assets_, which builds upon and extends Gregory Maxwell’s (and others’) work on Confidential Transactions (CT).

